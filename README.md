mackerel-plugin-tidb-process-list
=================================

TiDB 向けの [mackerel.io](https://mackerel.io) カスタムメトリクスプラグインです。

本プラグインは `INFORMATION_SCHEMA.CLUSTER_PROCESSLIST` と `INFORMATION_SCHEMA.CLUSTER_STATEMENTS_SUMMARY` からメトリクスを収集し、クラスター全体の TiDB ノードで実行中のプロセス情報とクエリレイテンシを把握できます。

## Synopsis

```shell
mackerel-plugin-tidb-process-list [-host=<host>] [-port=<port>] [-username=<username>] [-password=<password>] [-tempfile=<tempfile>] [-metric-key-prefix=<prefix>] [-tls=true] [-tls-root-cert=<filename>] [-tls-skip-verify=true]
```

## mackerel-agent.conf の記述例

```
[plugin.metrics.tidb-process-list]
command = "/path/to/mackerel-plugin-tidb-process-list -host=127.0.0.1 -port=4000 -username=root -password=YOUR_PASSWORD"
```

## 収集するメトリクス

### 接続状態（State）

- `State_autocommit`
- `State_in_transaction`
- `State_locked`
- `State_committing`
- `State_waiting_for_lock`
- `State_other`

### コマンド種別（Command）

- `Command_Query`
- `Command_Sleep`
- `Command_Execute`
- `Command_Other`

### 実行時間分布（Time Distribution）

- `Time_0_1s`
- `Time_1_10s`
- `Time_10_60s`
- `Time_over_60s`

### リソース使用量

- `TotalMem` (bytes)
- `TotalDisk` (bytes)

### CPU 時間

- `TotalTiDBCPU` (nanoseconds)
- `TotalTiKVCPU` (nanoseconds)

### 影響を受けた行数

- `TotalRowsAffected`

### クエリレイテンシ（STMT_TYPE 別）

`INFORMATION_SCHEMA.CLUSTER_STATEMENTS_SUMMARY` から取得した、クラスター全体の SQL 実行統計を STMT_TYPE ごとにグルーピングしています。

| STMT_TYPE グループ | 含まれる STMT_TYPE |
|---|---|
| `Select` | Select, PointGet, BatchPointGet |
| `Insert` | Insert, Replace, ImportInto |
| `Update` | Update |
| `Delete` | Delete |
| `DDL` | CreateTable, CreateIndex, AlterTable, DropTable, TruncateTable など |
| `Transaction` | Begin, Commit, Rollback |
| `Other` | Set, Show, ExplainSQL, Execute など |

#### 平均レイテンシ

- `AvgLatency_Select` (ミリ秒)
- `AvgLatency_Insert` (ミリ秒)
- `AvgLatency_Update` (ミリ秒)
- `AvgLatency_Delete` (ミリ秒)
- `AvgLatency_DDL` (ミリ秒)
- `AvgLatency_Transaction` (ミリ秒)
- `AvgLatency_Other` (ミリ秒)

#### 最大レイテンシ

- `MaxLatency_Select` (ミリ秒)
- `MaxLatency_Insert` (ミリ秒)
- `MaxLatency_Update` (ミリ秒)
- `MaxLatency_Delete` (ミリ秒)
- `MaxLatency_DDL` (ミリ秒)
- `MaxLatency_Transaction` (ミリ秒)
- `MaxLatency_Other` (ミリ秒)

#### 実行回数（1分差分）

- `ExecCount_Select`
- `ExecCount_Insert`
- `ExecCount_Update`
- `ExecCount_Delete`
- `ExecCount_DDL`
- `ExecCount_Transaction`
- `ExecCount_Other`

### 影響を受けた行数

- `TotalRowsAffected`

## 注意事項

- `TIDB_CPU` および `TIKV_CPU` カラムは、[Top SQL](https://docs.pingcap.com/tidb/stable/top-sql) 機能が有効な場合のみ意味のある値となります。有効でない場合は `0` となります。
- `CLUSTER_PROCESSLIST` はクラスター内の全 TiDB ノードを問い合わせ、プロセス状態を統一的なビューで提供します。
- `CLUSTER_STATEMENTS_SUMMARY` は SQL ダイジェストごとに集計された統計情報です。`tidb_stmt_summary_max_sql_length` などのシステム変数で設定内容を調整できます。

## 参考資料

- [TiDB ドキュメント - PROCESSLIST (CLUSTER_PROCESSLIST)](https://docs.pingcap.com/ja/tidb/stable/information-schema-processlist/#cluster_processlist)
- [TiDB ドキュメント - STATEMENTS_SUMMARY](https://docs.pingcap.com/tidb/stable/information-schema-statements-summary/)
- [Top SQL 機能](https://docs.pingcap.com/tidb/stable/top-sql)
- [Mackerel mackerel-plugin-mysql ドキュメント](https://mackerel.io/ja/docs/entry/plugins/mackerel-plugin-mysql)
