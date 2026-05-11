package tidbprocesslist

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func ptrInt64(v int64) *int64 {
	return &v
}

func ptrString(v string) *string {
	return &v
}

func TestNewStat(t *testing.T) {
	p := &TiDBProcessListPlugin{}
	stat := p.newStat()

	for k := range knownStates {
		assert.Contains(t, stat, k, "stat should contain %s", k)
		assert.EqualValues(t, 0, stat[k], "%s should be initialized to 0", k)
	}
	for k := range knownCommands {
		assert.Contains(t, stat, k, "stat should contain %s", k)
		assert.EqualValues(t, 0, stat[k], "%s should be initialized to 0", k)
	}

	assert.EqualValues(t, 0, stat["Time_0_1s"])
	assert.EqualValues(t, 0, stat["Time_1_10s"])
	assert.EqualValues(t, 0, stat["Time_10_60s"])
	assert.EqualValues(t, 0, stat["Time_over_60s"])
	assert.EqualValues(t, 0, stat["TotalMem"])
	assert.EqualValues(t, 0, stat["TotalDisk"])
	assert.EqualValues(t, 0, stat["TotalTiDBCPU"])
	assert.EqualValues(t, 0, stat["TotalTiKVCPU"])
	assert.EqualValues(t, 0, stat["TotalRowsAffected"])

	for _, g := range stmtGroups {
		assert.EqualValues(t, 0, stat["AvgLatency_"+g])
		assert.EqualValues(t, 0, stat["MaxLatency_"+g])
		assert.EqualValues(t, 0, stat["ExecCount_"+g])
	}
}

func TestCountState(t *testing.T) {
	p := &TiDBProcessListPlugin{}
	stat := p.newStat()

	p.countState(ptrString("autocommit"), stat)
	p.countState(ptrString("in transaction"), stat)
	p.countState(ptrString("Committing"), stat)
	p.countState(ptrString("locked"), stat)
	p.countState(ptrString("Waiting for table lock"), stat)
	p.countState(ptrString("unknown state"), stat)
	p.countState(nil, stat)

	assert.EqualValues(t, 1, stat["State_autocommit"])
	assert.EqualValues(t, 1, stat["State_in_transaction"])
	assert.EqualValues(t, 1, stat["State_committing"])
	assert.EqualValues(t, 1, stat["State_locked"])
	assert.EqualValues(t, 1, stat["State_waiting_for_lock"])
	assert.EqualValues(t, 2, stat["State_other"])
}

func TestCountCommand(t *testing.T) {
	p := &TiDBProcessListPlugin{}
	stat := p.newStat()

	p.countCommand("Query", stat)
	p.countCommand("Sleep", stat)
	p.countCommand("Execute", stat)
	p.countCommand("Unknown", stat)

	assert.EqualValues(t, 1, stat["Command_Query"])
	assert.EqualValues(t, 1, stat["Command_Sleep"])
	assert.EqualValues(t, 1, stat["Command_Execute"])
	assert.EqualValues(t, 1, stat["Command_Other"])
}

func TestCountTime(t *testing.T) {
	p := &TiDBProcessListPlugin{}
	stat := p.newStat()

	p.countTime(0, stat)
	p.countTime(1, stat)
	p.countTime(9, stat)
	p.countTime(10, stat)
	p.countTime(59, stat)
	p.countTime(60, stat)

	assert.EqualValues(t, 1, stat["Time_0_1s"])
	assert.EqualValues(t, 2, stat["Time_1_10s"])
	assert.EqualValues(t, 2, stat["Time_10_60s"])
	assert.EqualValues(t, 1, stat["Time_over_60s"])
}

func TestAggregateRow(t *testing.T) {
	p := &TiDBProcessListPlugin{}
	stat := p.newStat()

	p.aggregateRow(processRow{
		Command:      "Query",
		State:        ptrString("autocommit"),
		Time:         5,
		Mem:          ptrInt64(1024),
		Disk:         ptrInt64(256),
		TiDBCPU:      ptrInt64(100),
		TiKVCPU:      ptrInt64(200),
		RowsAffected: ptrInt64(10),
	}, stat)

	assert.EqualValues(t, 1, stat["State_autocommit"])
	assert.EqualValues(t, 1, stat["Command_Query"])
	assert.EqualValues(t, 1, stat["Time_1_10s"])
	assert.EqualValues(t, 1024, stat["TotalMem"])
	assert.EqualValues(t, 256, stat["TotalDisk"])
	assert.EqualValues(t, 100, stat["TotalTiDBCPU"])
	assert.EqualValues(t, 200, stat["TotalTiKVCPU"])
	assert.EqualValues(t, 10, stat["TotalRowsAffected"])
}

func TestAggregateRowMultiple(t *testing.T) {
	p := &TiDBProcessListPlugin{}
	stat := p.newStat()

	rows := []processRow{
		{Command: "Query", State: ptrString("autocommit"), Time: 0, Mem: ptrInt64(100)},
		{Command: "Sleep", State: ptrString("in transaction"), Time: 5, Mem: ptrInt64(200)},
		{Command: "Query", State: ptrString("unknown"), Time: 15, Mem: ptrInt64(300)},
		{Command: "Execute", State: nil, Time: 120, Mem: ptrInt64(400)},
	}

	for _, r := range rows {
		p.aggregateRow(r, stat)
	}

	assert.EqualValues(t, 1, stat["State_autocommit"])
	assert.EqualValues(t, 1, stat["State_in_transaction"])
	assert.EqualValues(t, 2, stat["State_other"])

	assert.EqualValues(t, 2, stat["Command_Query"])
	assert.EqualValues(t, 1, stat["Command_Sleep"])
	assert.EqualValues(t, 1, stat["Command_Execute"])

	assert.EqualValues(t, 1, stat["Time_0_1s"])
	assert.EqualValues(t, 1, stat["Time_1_10s"])
	assert.EqualValues(t, 1, stat["Time_10_60s"])
	assert.EqualValues(t, 1, stat["Time_over_60s"])

	assert.EqualValues(t, 1000, stat["TotalMem"])
}

func TestAggregateRowNullPointers(t *testing.T) {
	p := &TiDBProcessListPlugin{}
	stat := p.newStat()

	p.aggregateRow(processRow{
		Command:      "Query",
		State:        nil,
		Time:         0,
		Mem:          nil,
		Disk:         nil,
		TiDBCPU:      nil,
		TiKVCPU:      nil,
		RowsAffected: nil,
	}, stat)

	assert.EqualValues(t, 1, stat["State_other"])
	assert.EqualValues(t, 0, stat["TotalMem"])
	assert.EqualValues(t, 0, stat["TotalDisk"])
	assert.EqualValues(t, 0, stat["TotalTiDBCPU"])
	assert.EqualValues(t, 0, stat["TotalTiKVCPU"])
	assert.EqualValues(t, 0, stat["TotalRowsAffected"])
}

func TestStmtTypeToGroup(t *testing.T) {
	assert.Equal(t, "Select", stmtTypeToGroup("Select"))
	assert.Equal(t, "Select", stmtTypeToGroup("PointGet"))
	assert.Equal(t, "Insert", stmtTypeToGroup("Insert"))
	assert.Equal(t, "Insert", stmtTypeToGroup("Replace"))
	assert.Equal(t, "Update", stmtTypeToGroup("Update"))
	assert.Equal(t, "Delete", stmtTypeToGroup("Delete"))
	assert.Equal(t, "DDL", stmtTypeToGroup("CreateTable"))
	assert.Equal(t, "DDL", stmtTypeToGroup("DropIndex"))
	assert.Equal(t, "Transaction", stmtTypeToGroup("Commit"))
	assert.Equal(t, "Transaction", stmtTypeToGroup("Begin"))
	assert.Equal(t, "Other", stmtTypeToGroup("ExplainSQL"))
	assert.Equal(t, "Other", stmtTypeToGroup("Show"))
	assert.Equal(t, "Other", stmtTypeToGroup("UnknownType"))
}

func TestAccumulateStmtSummaryByGroup(t *testing.T) {
	groups := make(map[string]*stmtSummaryAccumulator)
	for _, g := range stmtGroups {
		groups[g] = &stmtSummaryAccumulator{}
	}

	// Simulate accumulating rows for different stmt types
	rows := []struct {
		stmtType string
		avg      int64
		max      int64
		count    int64
	}{
		{"Select", 100, 200, 10},
		{"Select", 200, 150, 5},
		{"Insert", 50, 300, 20},
		{"Update", 1000, 5000, 2},
		{"Delete", 500, 1000, 3},
		{"CreateTable", 100, 200, 1},
		{"Commit", 10, 20, 100},
		{"ExplainSQL", 50, 100, 5},
	}

	for _, r := range rows {
		group := stmtTypeToGroup(r.stmtType)
		acc := groups[group]
		acc.weightedSum += r.avg * r.count
		acc.totalExecCount += r.count
		if r.max > acc.maxLatency {
			acc.maxLatency = r.max
		}
	}

	// Select: (100*10 + 200*5) / (10+5) = 2000/15 = 133.33, max = max(200,150) = 200
	assert.InDelta(t, 2000.0/15.0, float64(groups["Select"].weightedSum)/float64(groups["Select"].totalExecCount), 0.01)
	assert.EqualValues(t, 200, groups["Select"].maxLatency)
	assert.EqualValues(t, 15, groups["Select"].totalExecCount)

	// Insert: (50*20) / 20 = 50, max = 300
	assert.EqualValues(t, 50, float64(groups["Insert"].weightedSum)/float64(groups["Insert"].totalExecCount))
	assert.EqualValues(t, 300, groups["Insert"].maxLatency)
	assert.EqualValues(t, 20, groups["Insert"].totalExecCount)

	// Update: (1000*2) / 2 = 1000, max = 5000
	assert.EqualValues(t, 1000, float64(groups["Update"].weightedSum)/float64(groups["Update"].totalExecCount))
	assert.EqualValues(t, 5000, groups["Update"].maxLatency)

	// DDL (CreateTable): (100*1) / 1 = 100, max = 200
	assert.EqualValues(t, 100, float64(groups["DDL"].weightedSum)/float64(groups["DDL"].totalExecCount))
	assert.EqualValues(t, 200, groups["DDL"].maxLatency)

	// Transaction (Commit): (10*100) / 100 = 10, max = 20
	assert.EqualValues(t, 10, float64(groups["Transaction"].weightedSum)/float64(groups["Transaction"].totalExecCount))
	assert.EqualValues(t, 20, groups["Transaction"].maxLatency)

	// Other (ExplainSQL): (50*5) / 5 = 50, max = 100
	assert.EqualValues(t, 50, float64(groups["Other"].weightedSum)/float64(groups["Other"].totalExecCount))
	assert.EqualValues(t, 100, groups["Other"].maxLatency)
}

func TestGraphDefinition(t *testing.T) {
	p := &TiDBProcessListPlugin{prefix: "tidb.processlist"}
	graphs := p.GraphDefinition()

	assert.Contains(t, graphs, "tidb.processlist.state")
	assert.Contains(t, graphs, "tidb.processlist.command")
	assert.Contains(t, graphs, "tidb.processlist.time")
	assert.Contains(t, graphs, "tidb.processlist.resources")
	assert.Contains(t, graphs, "tidb.processlist.cpu")
	assert.Contains(t, graphs, "tidb.processlist.rows")
	assert.Contains(t, graphs, "tidb.processlist.latency_by_stmt")
	assert.Contains(t, graphs, "tidb.processlist.max_latency_by_stmt")
	assert.Contains(t, graphs, "tidb.processlist.exec_count_by_stmt")

	latencyGraph := graphs["tidb.processlist.latency_by_stmt"]
	assert.Equal(t, len(stmtGroups), len(latencyGraph.Metrics))
	assert.Equal(t, "float", latencyGraph.Unit)

	countGraph := graphs["tidb.processlist.exec_count_by_stmt"]
	assert.Equal(t, len(stmtGroups), len(countGraph.Metrics))
	assert.True(t, countGraph.Metrics[0].Diff)
}

func TestMetricKeyPrefixDefault(t *testing.T) {
	p := &TiDBProcessListPlugin{}
	assert.Equal(t, "tidb.processlist", p.MetricKeyPrefix())
}

func TestMetricKeyPrefixCustom(t *testing.T) {
	p := &TiDBProcessListPlugin{prefix: "custom"}
	assert.Equal(t, "custom", p.MetricKeyPrefix())
}
