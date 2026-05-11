package tidbprocesslist

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/go-sql-driver/mysql"
	mp "github.com/mackerelio/go-mackerel-plugin"
)

var knownStates = map[string]bool{
	"State_autocommit":       true,
	"State_in_transaction":   true,
	"State_locked":           true,
	"State_committing":       true,
	"State_waiting_for_lock": true,
	"State_other":            true,
}

var knownCommands = map[string]bool{
	"Command_Query":   true,
	"Command_Sleep":   true,
	"Command_Execute": true,
	"Command_Other":   true,
}

var stmtTypeGroups = map[string]string{
	"Select":         "Select",
	"PointGet":       "Select",
	"BatchPointGet":  "Select",
	"Insert":         "Insert",
	"Replace":        "Insert",
	"ImportInto":     "Insert",
	"Update":         "Update",
	"Delete":         "Delete",
	"AlterTable":     "DDL",
	"AnalyzeTable":   "DDL",
	"CompactTable":   "DDL",
	"CreateDatabase": "DDL",
	"CreateIndex":    "DDL",
	"CreateTable":    "DDL",
	"CreateView":     "DDL",
	"CreateUser":     "DDL",
	"DropDatabase":   "DDL",
	"DropIndex":      "DDL",
	"DropTable":      "DDL",
	"DropView":       "DDL",
	"TruncateTable":  "DDL",
	"SplitRegion":    "DDL",
	"FlashBackTable": "DDL",
	"RecoverTable":   "DDL",
	"Begin":          "Transaction",
	"Commit":         "Transaction",
	"Rollback":       "Transaction",
}

var stmtGroups = []string{"Select", "Insert", "Update", "Delete", "DDL", "Transaction", "Other"}

var latencyScale = 1.0 / 1000000.0

// TiDBProcessListPlugin collects TiDB CLUSTER_PROCESSLIST metrics.
type TiDBProcessListPlugin struct {
	Target   string
	Tempfile string
	prefix   string
	Username string
	Password string

	EnableTLS     bool
	TLSRootCert   string
	TLSSkipVerify bool
}

// MetricKeyPrefix returns the metrics key prefix.
func (p *TiDBProcessListPlugin) MetricKeyPrefix() string {
	if p.prefix == "" {
		p.prefix = "tidb.processlist"
	}
	return p.prefix
}

// FetchMetrics queries CLUSTER_PROCESSLIST and aggregates metrics.
func (p *TiDBProcessListPlugin) FetchMetrics() (map[string]float64, error) {
	config := mysql.NewConfig()
	config.User = p.Username
	config.Passwd = p.Password
	config.Net = "tcp"
	config.Addr = p.Target

	if p.EnableTLS {
		if err := p.setupTLS(config); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	defer db.Close() //nolint

	stat, err := p.fetchClusterProcesslist(db)
	if err != nil {
		return nil, err
	}

	if err := p.fetchStatementsSummary(db, stat); err != nil {
		return nil, err
	}

	return stat, nil
}

func (p *TiDBProcessListPlugin) setupTLS(config *mysql.Config) error {
	var c tls.Config
	if p.TLSRootCert != "" {
		certPool := x509.NewCertPool()
		pem, err := os.ReadFile(p.TLSRootCert)
		if err != nil {
			return fmt.Errorf("cannot read %s: %v", p.TLSRootCert, err)
		}
		certPool.AppendCertsFromPEM(pem)
		c.RootCAs = certPool
	}
	c.InsecureSkipVerify = p.TLSSkipVerify
	if err := mysql.RegisterTLSConfig("custom", &c); err != nil {
		return err
	}
	config.TLSConfig = "custom"
	return nil
}

type processRow struct {
	Instance     string
	Command      string
	State        *string
	Time         int
	Mem          *int64
	Disk         *int64
	TiDBCPU      *int64
	TiKVCPU      *int64
	RowsAffected *int64
}

func (p *TiDBProcessListPlugin) fetchClusterProcesslist(db *sql.DB) (map[string]float64, error) {
	query := `SELECT INSTANCE, COMMAND, STATE, TIME, MEM, DISK, TIDB_CPU, TIKV_CPU, ROWS_AFFECTED
			  FROM INFORMATION_SCHEMA.CLUSTER_PROCESSLIST`
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query cluster_processlist: %w", err)
	}
	defer rows.Close() //nolint

	stat := p.newStat()

	for rows.Next() {
		var r processRow
		if err := rows.Scan(
			&r.Instance,
			&r.Command,
			&r.State,
			&r.Time,
			&r.Mem,
			&r.Disk,
			&r.TiDBCPU,
			&r.TiKVCPU,
			&r.RowsAffected,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		p.aggregateRow(r, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	return stat, nil
}

type stmtSummaryAccumulator struct {
	weightedSum    int64
	totalExecCount int64
	maxLatency     int64
}

func (p *TiDBProcessListPlugin) fetchStatementsSummary(db *sql.DB, stat map[string]float64) error {
	query := `SELECT STMT_TYPE, AVG_LATENCY, MAX_LATENCY, EXEC_COUNT
			  FROM INFORMATION_SCHEMA.CLUSTER_STATEMENTS_SUMMARY`
	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("query cluster_statements_summary: %w", err)
	}
	defer rows.Close() //nolint

	groups := make(map[string]*stmtSummaryAccumulator)
	for _, g := range stmtGroups {
		groups[g] = &stmtSummaryAccumulator{}
	}

	for rows.Next() {
		var stmtType string
		var avgLatency int64
		var rowMaxLatency int64
		var execCount int64
		if err := rows.Scan(&stmtType, &avgLatency, &rowMaxLatency, &execCount); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}
		group := stmtTypeToGroup(stmtType)
		acc := groups[group]
		acc.weightedSum += avgLatency * execCount
		acc.totalExecCount += execCount
		if rowMaxLatency > acc.maxLatency {
			acc.maxLatency = rowMaxLatency
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}

	for _, g := range stmtGroups {
		acc := groups[g]
		avgKey := "AvgLatency_" + g
		maxKey := "MaxLatency_" + g
		countKey := "ExecCount_" + g
		if acc.totalExecCount > 0 {
			stat[avgKey] = float64(acc.weightedSum) / float64(acc.totalExecCount)
		} else {
			stat[avgKey] = 0
		}
		stat[maxKey] = float64(acc.maxLatency)
		stat[countKey] = float64(acc.totalExecCount)
	}

	return nil
}

func stmtTypeToGroup(stmtType string) string {
	if group, ok := stmtTypeGroups[stmtType]; ok {
		return group
	}
	return "Other"
}

func (p *TiDBProcessListPlugin) newStat() map[string]float64 {
	stat := make(map[string]float64)

	for k := range knownStates {
		stat[k] = 0
	}
	for k := range knownCommands {
		stat[k] = 0
	}

	stat["Time_0_1s"] = 0
	stat["Time_1_10s"] = 0
	stat["Time_10_60s"] = 0
	stat["Time_over_60s"] = 0

	stat["TotalMem"] = 0
	stat["TotalDisk"] = 0
	stat["TotalTiDBCPU"] = 0
	stat["TotalTiKVCPU"] = 0
	stat["TotalRowsAffected"] = 0

	for _, g := range stmtGroups {
		stat["AvgLatency_"+g] = 0
		stat["MaxLatency_"+g] = 0
		stat["ExecCount_"+g] = 0
	}

	return stat
}

func (p *TiDBProcessListPlugin) aggregateRow(r processRow, stat map[string]float64) {
	p.countState(r.State, stat)
	p.countCommand(r.Command, stat)
	p.countTime(r.Time, stat)

	if r.Mem != nil {
		stat["TotalMem"] += float64(*r.Mem)
	}
	if r.Disk != nil {
		stat["TotalDisk"] += float64(*r.Disk)
	}
	if r.TiDBCPU != nil {
		stat["TotalTiDBCPU"] += float64(*r.TiDBCPU)
	}
	if r.TiKVCPU != nil {
		stat["TotalTiKVCPU"] += float64(*r.TiKVCPU)
	}
	if r.RowsAffected != nil {
		stat["TotalRowsAffected"] += float64(*r.RowsAffected)
	}
}

func (p *TiDBProcessListPlugin) countState(rawState *string, stat map[string]float64) {
	state := "NULL"
	if rawState != nil {
		state = *rawState
	}

	if state == "" {
		state = "none"
	} else if state == "Table lock" {
		state = "locked"
	} else if strings.HasPrefix(state, "Waiting for ") && strings.HasSuffix(state, "lock") {
		state = "waiting_for_lock"
	}

	key := "State_" + strings.ReplaceAll(strings.ToLower(state), " ", "_")
	if _, ok := knownStates[key]; !ok {
		key = "State_other"
	}

	stat[key]++
}

func (p *TiDBProcessListPlugin) countCommand(command string, stat map[string]float64) {
	key := "Command_" + strings.Title(strings.ToLower(command))
	if _, ok := knownCommands[key]; !ok {
		key = "Command_Other"
	}

	stat[key]++
}

func (p *TiDBProcessListPlugin) countTime(t int, stat map[string]float64) {
	switch {
	case t < 1:
		stat["Time_0_1s"]++
	case t < 10:
		stat["Time_1_10s"]++
	case t < 60:
		stat["Time_10_60s"]++
	default:
		stat["Time_over_60s"]++
	}
}

// GraphDefinition returns graph definitions for mackerel-plugin.
func (p *TiDBProcessListPlugin) GraphDefinition() map[string]mp.Graphs {
	prefix := p.MetricKeyPrefix()

	latencyMetrics := make([]mp.Metrics, 0, len(stmtGroups))
	maxLatencyMetrics := make([]mp.Metrics, 0, len(stmtGroups))
	execCountMetrics := make([]mp.Metrics, 0, len(stmtGroups))

	for _, g := range stmtGroups {
		latencyMetrics = append(latencyMetrics, mp.Metrics{
			Name:  "AvgLatency_" + g,
			Label: g,
			Diff:  false, Stacked: false, Scale: latencyScale,
		})
		maxLatencyMetrics = append(maxLatencyMetrics, mp.Metrics{
			Name:  "MaxLatency_" + g,
			Label: g,
			Diff:  false, Stacked: false, Scale: latencyScale,
		})
		execCountMetrics = append(execCountMetrics, mp.Metrics{
			Name:  "ExecCount_" + g,
			Label: g,
			Diff:  true, Stacked: false,
		})
	}

	return map[string]mp.Graphs{
		prefix + ".state": {
			Label: prefix + " State",
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "State_autocommit", Label: "autocommit", Diff: false, Stacked: true},
				{Name: "State_in_transaction", Label: "in transaction", Diff: false, Stacked: true},
				{Name: "State_locked", Label: "locked", Diff: false, Stacked: true},
				{Name: "State_committing", Label: "committing", Diff: false, Stacked: true},
				{Name: "State_waiting_for_lock", Label: "waiting for lock", Diff: false, Stacked: true},
				{Name: "State_other", Label: "other", Diff: false, Stacked: true},
			},
		},
		prefix + ".command": {
			Label: prefix + " Command",
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "Command_Query", Label: "Query", Diff: false, Stacked: true},
				{Name: "Command_Sleep", Label: "Sleep", Diff: false, Stacked: true},
				{Name: "Command_Execute", Label: "Execute", Diff: false, Stacked: true},
				{Name: "Command_Other", Label: "Other", Diff: false, Stacked: true},
			},
		},
		prefix + ".time": {
			Label: prefix + " Time Distribution",
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "Time_0_1s", Label: "0-1s", Diff: false, Stacked: true},
				{Name: "Time_1_10s", Label: "1-10s", Diff: false, Stacked: true},
				{Name: "Time_10_60s", Label: "10-60s", Diff: false, Stacked: true},
				{Name: "Time_over_60s", Label: "60s+", Diff: false, Stacked: true},
			},
		},
		prefix + ".resources": {
			Label: prefix + " Resources",
			Unit:  "bytes",
			Metrics: []mp.Metrics{
				{Name: "TotalMem", Label: "Total Memory", Diff: false, Stacked: false},
				{Name: "TotalDisk", Label: "Total Disk", Diff: false, Stacked: false},
			},
		},
		prefix + ".cpu": {
			Label: prefix + " CPU Time",
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "TotalTiDBCPU", Label: "TiDB CPU (ns)", Diff: true, Stacked: false},
				{Name: "TotalTiKVCPU", Label: "TiKV CPU (ns)", Diff: true, Stacked: false},
			},
		},
		prefix + ".rows": {
			Label: prefix + " Rows Affected",
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "TotalRowsAffected", Label: "Rows Affected", Diff: false, Stacked: false},
			},
		},
		prefix + ".latency_by_stmt": {
			Label:   prefix + " Latency by Statement Type",
			Unit:    "float",
			Metrics: latencyMetrics,
		},
		prefix + ".max_latency_by_stmt": {
			Label:   prefix + " Max Latency by Statement Type",
			Unit:    "float",
			Metrics: maxLatencyMetrics,
		},
		prefix + ".exec_count_by_stmt": {
			Label:   prefix + " Execution Count by Statement Type",
			Unit:    "integer",
			Metrics: execCountMetrics,
		},
	}
}

// Do is the entry point.
func Do() {
	optHost := flag.String("host", "localhost", "Hostname")
	optPort := flag.String("port", "4000", "Port")
	optUser := flag.String("username", "root", "Username")
	optPass := flag.String("password", os.Getenv("TIDB_PASSWORD"), "Password")
	optTempfile := flag.String("tempfile", "", "Temp file name")
	optMetricKeyPrefix := flag.String("metric-key-prefix", "tidb.processlist", "Metric key prefix")
	optEnableTLS := flag.Bool("tls", false, "Enables TLS connection")
	optTLSRootCert := flag.String("tls-root-cert", "", "The root certificate used for TLS certificate verification")
	optTLSSkipVerify := flag.Bool("tls-skip-verify", false, "Disable TLS certificate verification")

	flag.Parse()

	plugin := &TiDBProcessListPlugin{
		Target:        net.JoinHostPort(*optHost, *optPort),
		Username:      *optUser,
		Password:      *optPass,
		prefix:        *optMetricKeyPrefix,
		EnableTLS:     *optEnableTLS,
		TLSRootCert:   *optTLSRootCert,
		TLSSkipVerify: *optTLSSkipVerify,
	}

	helper := mp.NewMackerelPlugin(plugin)
	helper.Tempfile = *optTempfile
	helper.Run()
}
