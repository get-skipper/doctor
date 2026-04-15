package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

var version = "dev"

type status int

const (
	statusOK status = iota
	statusWarn
	statusError
	statusSkip
)

type result struct {
	label  string
	status status
	detail string
}

type appConfig struct {
	spreadsheetID   string
	credentialsPath string
	credentialsB64  string
	sheetName       string
	referenceSheets []string
}

const sheetsScope = "https://www.googleapis.com/auth/spreadsheets"

const (
	colorGreen  = 32
	colorYellow = 33
	colorRed    = 31
	colorGray   = 90
)

func main() {
	var spreadsheetID, sheetName string
	credentialsPath := flag.String("credentials", os.Getenv("GOOGLE_CREDENTIALS_FILE"), "Path to service account JSON")
	flag.StringVar(&spreadsheetID, "spreadsheet-id", os.Getenv("SKIPPER_SPREADSHEET_ID"), "Spreadsheet ID")
	flag.StringVar(&sheetName, "sheet-name", os.Getenv("SKIPPER_SHEET_NAME"), "Sheet name (optional, uses first sheet if empty)")
	refSheets := flag.String("reference-sheets", os.Getenv("SKIPPER_REFERENCE_SHEETS"), "Comma-separated reference sheet names (optional)")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	cfg := appConfig{
		spreadsheetID:   spreadsheetID,
		credentialsPath: *credentialsPath,
		credentialsB64:  os.Getenv("GOOGLE_CREDS_B64"),
		sheetName:       sheetName,
	}

	if *refSheets != "" {
		cfg.referenceSheets = strings.Split(*refSheets, ",")
		for i, s := range cfg.referenceSheets {
			cfg.referenceSheets[i] = strings.TrimSpace(s)
		}
	}

	if cfg.spreadsheetID == "" {
		fmt.Fprintf(os.Stderr, "Error: spreadsheet-id required (set via --spreadsheet-id or SKIPPER_SPREADSHEET_ID)\n")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("\nSkipper doctor — checking your setup")

	results := runChecks(ctx, cfg)
	printSummary(results)

	hasError := false
	for _, r := range results {
		if r.status == statusWarn || r.status == statusError {
			hasError = true
			break
		}
	}

	if hasError {
		os.Exit(1)
	}
}

func runChecks(ctx context.Context, cfg appConfig) []result {
	var results []result

	// Step 1: Credentials
	credJSON, res := checkCredentials(cfg)
	results = append(results, res)
	printResult(res)
	if res.status == statusError {
		return results
	}

	// Step 2: Sheets API service
	svc, res := buildSheetsService(ctx, credJSON)
	results = append(results, res)
	printResult(res)
	if res.status == statusError {
		return results
	}

	// Step 2b: API reachability
	spreadsheet, latency, res := checkAPIReachable(ctx, svc, cfg.spreadsheetID)
	results = append(results, res)
	printResult(res)
	if res.status == statusError {
		return results
	}

	// Step 3: Spreadsheet accessible (reuses previous result)
	res = result{
		label:  "Spreadsheet accessible",
		status: statusOK,
		detail: fmt.Sprintf("id: %s (%dms)", cfg.spreadsheetID, latency.Milliseconds()),
	}
	results = append(results, res)
	printResult(res)

	// Step 4: Tab existence
	values, resolvedName, res := checkSheetTabFound(ctx, svc, cfg.spreadsheetID, cfg.sheetName, spreadsheet)
	results = append(results, res)
	printResult(res)
	if res.status == statusError {
		return results
	}

	// Step 5: Schema
	_, disabledUntilCol, res := checkSchemaValid(values, resolvedName)
	results = append(results, res)
	printResult(res)
	if res.status == statusError {
		return results
	}

	// Step 6: Date formats
	res = checkDateFormats(values, disabledUntilCol)
	results = append(results, res)
	printResult(res)

	// Step 7: Write permission
	res = checkWritePermission(ctx, svc, cfg.spreadsheetID)
	results = append(results, res)
	printResult(res)

	// Step 8: Reference sheets
	if len(cfg.referenceSheets) > 0 {
		refResults := checkReferenceSheets(ctx, svc, cfg.spreadsheetID, cfg.referenceSheets)
		results = append(results, refResults...)
		for _, r := range refResults {
			printResult(r)
		}
	}

	return results
}

func checkCredentials(cfg appConfig) ([]byte, result) {
	var raw []byte
	var err error

	switch {
	case cfg.credentialsPath != "":
		raw, err = os.ReadFile(cfg.credentialsPath)
	case cfg.credentialsB64 != "":
		raw, err = base64.StdEncoding.DecodeString(cfg.credentialsB64)
	default:
		return nil, result{
			label:  "Credentials parsed",
			status: statusError,
			detail: "no credentials provided (set GOOGLE_CREDS_B64 or GOOGLE_CREDENTIALS_FILE)",
		}
	}

	if err != nil {
		return nil, result{
			label:  "Credentials parsed",
			status: statusError,
			detail: err.Error(),
		}
	}

	// Extract client_email and verify service account type
	var sa struct {
		Type        string `json:"type"`
		ClientEmail string `json:"client_email"`
	}
	if err := json.Unmarshal(raw, &sa); err != nil {
		return nil, result{
			label:  "Credentials parsed",
			status: statusError,
			detail: "invalid JSON: " + err.Error(),
		}
	}

	if sa.Type != "service_account" {
		return nil, result{
			label:  "Credentials parsed",
			status: statusError,
			detail: fmt.Sprintf("expected type 'service_account', got %q", sa.Type),
		}
	}

	// Verify with Google
	verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer verifyCancel()
	if _, err := google.CredentialsFromJSON(verifyCtx, raw, sheetsScope); err != nil {
		return nil, result{
			label:  "Credentials parsed",
			status: statusError,
			detail: "invalid credentials: " + err.Error(),
		}
	}

	return raw, result{
		label:  "Credentials parsed",
		status: statusOK,
		detail: fmt.Sprintf("service account: %s", sa.ClientEmail),
	}
}

func buildSheetsService(ctx context.Context, credJSON []byte) (*sheets.Service, result) {
	creds, err := google.CredentialsFromJSON(ctx, credJSON, sheetsScope)
	if err != nil {
		return nil, result{
			label:  "Sheets API credentials",
			status: statusError,
			detail: err.Error(),
		}
	}

	svc, err := sheets.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, result{
			label:  "Sheets API credentials",
			status: statusError,
			detail: "cannot create Sheets service: " + err.Error(),
		}
	}

	return svc, result{
		label:  "Sheets API credentials",
		status: statusOK,
		detail: "authenticated",
	}
}

func checkAPIReachable(ctx context.Context, svc *sheets.Service, spreadsheetID string) (*sheets.Spreadsheet, time.Duration, result) {
	start := time.Now()
	spreadsheet, err := svc.Spreadsheets.Get(spreadsheetID).
		Fields("spreadsheetId", "sheets").
		Context(ctx).
		Do()
	latency := time.Since(start)

	if err != nil {
		var gErr *googleapi.Error
		if errors.As(err, &gErr) {
			if gErr.Code == 403 || gErr.Code == 401 {
				return nil, latency, result{
					label:  "Sheets API reachable",
					status: statusError,
					detail: fmt.Sprintf("HTTP %d — check IAM permissions (%dms)", gErr.Code, latency.Milliseconds()),
				}
			}
			return nil, latency, result{
				label:  "Sheets API reachable",
				status: statusError,
				detail: fmt.Sprintf("HTTP %d (%dms)", gErr.Code, latency.Milliseconds()),
			}
		}
		return nil, latency, result{
			label:  "Sheets API reachable",
			status: statusError,
			detail: fmt.Sprintf("network error: %v", err),
		}
	}

	return spreadsheet, latency, result{
		label:  "Sheets API reachable",
		status: statusOK,
		detail: fmt.Sprintf("200 in %dms", latency.Milliseconds()),
	}
}

func checkSheetTabFound(ctx context.Context, svc *sheets.Service, spreadsheetID string, sheetName string, spreadsheet *sheets.Spreadsheet) (*sheets.ValueRange, string, result) {
	resolvedName := sheetName
	if resolvedName == "" && len(spreadsheet.Sheets) > 0 {
		resolvedName = spreadsheet.Sheets[0].Properties.Title
	}

	// Verify sheet exists
	found := false
	for _, s := range spreadsheet.Sheets {
		if s.Properties.Title == resolvedName {
			found = true
			break
		}
	}
	if !found {
		return nil, "", result{
			label:  "Sheet tab found",
			status: statusError,
			detail: fmt.Sprintf("tab %q not found", resolvedName),
		}
	}

	// Quote sheet name if needed
	quotedName := quoteSheetName(resolvedName)

	// Fetch values
	vr, err := svc.Spreadsheets.Values.Get(spreadsheetID, quotedName).
		Context(ctx).
		Do()
	if err != nil {
		return nil, "", result{
			label:  "Sheet tab found",
			status: statusError,
			detail: err.Error(),
		}
	}

	totalRows := len(vr.Values)
	return vr, resolvedName, result{
		label:  "Sheet tab found",
		status: statusOK,
		detail: fmt.Sprintf("%q, %d rows", resolvedName, totalRows),
	}
}

func quoteSheetName(name string) string {
	if strings.ContainsAny(name, " '") {
		return "'" + strings.ReplaceAll(name, "'", "''") + "'"
	}
	return name
}

func checkSchemaValid(vr *sheets.ValueRange, sheetName string) (int, int, result) {
	if len(vr.Values) == 0 {
		return -1, -1, result{
			label:  "Schema valid",
			status: statusError,
			detail: "sheet is empty",
		}
	}

	header := toStringSlice(vr.Values[0])
	testIDIdx := indexOf(header, "testId")
	disabledUntilIdx := indexOf(header, "disabledUntil")

	if testIDIdx < 0 || disabledUntilIdx < 0 {
		missing := []string{}
		if testIDIdx < 0 {
			missing = append(missing, "testId")
		}
		if disabledUntilIdx < 0 {
			missing = append(missing, "disabledUntil")
		}
		return -1, -1, result{
			label:  "Schema valid",
			status: statusError,
			detail: fmt.Sprintf("missing columns: %s", strings.Join(missing, ", ")),
		}
	}

	return testIDIdx, disabledUntilIdx, result{
		label:  "Schema valid",
		status: statusOK,
		detail: strings.Join(header, ", "),
	}
}

func checkDateFormats(vr *sheets.ValueRange, disabledUntilCol int) result {
	if disabledUntilCol == -1 {
		return result{
			label:  "Date formats",
			status: statusSkip,
			detail: "disabledUntil column not found",
		}
	}

	var badRows []int
	for i, row := range vr.Values[1:] {
		rowNum := i + 2 // 1-indexed; row 1 is header
		if disabledUntilCol >= len(row) {
			continue
		}

		val, _ := row[disabledUntilCol].(string)
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}

		if _, err := time.Parse("2006-01-02", val); err != nil {
			badRows = append(badRows, rowNum)
		}
	}

	if len(badRows) == 0 {
		return result{
			label:  "Date formats",
			status: statusOK,
			detail: "all valid",
		}
	}

	rowStrs := make([]string, len(badRows))
	for i, r := range badRows {
		rowStrs[i] = strconv.Itoa(r)
	}
	return result{
		label:  "Date formats",
		status: statusWarn,
		detail: fmt.Sprintf("%d invalid (rows %s — use YYYY-MM-DD)", len(badRows), strings.Join(rowStrs, ", ")),
	}
}

func checkWritePermission(ctx context.Context, svc *sheets.Service, spreadsheetID string) result {
	req := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{
			{
				AppendDimension: &sheets.AppendDimensionRequest{
					SheetId:   0,
					Dimension: "ROWS",
					Length:    0, // append 0 rows — tests write permission without modifying sheet
				},
			},
		},
	}
	_, err := svc.Spreadsheets.BatchUpdate(spreadsheetID, req).
		Context(ctx).
		Do()

	if err == nil {
		return result{
			label:  "Write permission",
			status: statusOK,
			detail: "editor role",
		}
	}

	var gErr *googleapi.Error
	if errors.As(err, &gErr) && gErr.Code == 403 {
		return result{
			label:  "Write permission",
			status: statusWarn,
			detail: "viewer role — sync mode will fail",
		}
	}

	return result{
		label:  "Write permission",
		status: statusWarn,
		detail: fmt.Sprintf("unexpected error: %v", err),
	}
}

func checkReferenceSheets(ctx context.Context, svc *sheets.Service, spreadsheetID string, names []string) []result {
	var results []result
	for _, name := range names {
		quotedName := quoteSheetName(name)
		vr, err := svc.Spreadsheets.Values.Get(spreadsheetID, quotedName).
			Context(ctx).
			Do()
		if err != nil {
			results = append(results, result{
				label:  fmt.Sprintf("Reference sheet %q", name),
				status: statusError,
				detail: err.Error(),
			})
			continue
		}

		rowCount := len(vr.Values)
		results = append(results, result{
			label:  fmt.Sprintf("Reference sheet %q", name),
			status: statusOK,
			detail: fmt.Sprintf("%d rows", rowCount),
		})
	}
	return results
}

func colorize(s string, code int) string {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return s
	}
	return fmt.Sprintf("\033[%dm%s\033[0m", code, s)
}

func printResult(r result) {
	var icon, label string
	switch r.status {
	case statusOK:
		icon = colorize("✓", colorGreen)
		label = colorize(r.label, colorGreen)
	case statusWarn:
		icon = colorize("⚠", colorYellow)
		label = colorize(r.label, colorYellow)
	case statusError:
		icon = colorize("✗", colorRed)
		label = colorize(r.label, colorRed)
	case statusSkip:
		icon = colorize("-", colorGray)
		label = colorize(r.label, colorGray)
	}
	fmt.Printf("  %s  %-36s %s\n", icon, label, r.detail)
}

func printSummary(results []result) {
	fmt.Println()
	var warnings, errors int
	for _, r := range results {
		if r.status == statusWarn {
			warnings++
		} else if r.status == statusError {
			errors++
		}
	}

	if warnings == 0 && errors == 0 {
		fmt.Println(colorize("All checks passed.", colorGreen))
	} else {
		total := warnings + errors
		fmt.Printf("%d %s found.\n", total, colorize("issues", colorYellow))
		if errors > 0 {
			fmt.Printf("  • %d error(s)\n", errors)
		}
		if warnings > 0 {
			fmt.Printf("  • %d warning(s) — fix before enabling sync mode\n", warnings)
		}
	}
	fmt.Println()
}

func toStringSlice(row []any) []string {
	s := make([]string, len(row))
	for i, v := range row {
		if str, ok := v.(string); ok {
			s[i] = str
		} else {
			s[i] = fmt.Sprintf("%v", v)
		}
	}
	return s
}

func indexOf(header []string, col string) int {
	for i, h := range header {
		if h == col {
			return i
		}
	}
	return -1
}
