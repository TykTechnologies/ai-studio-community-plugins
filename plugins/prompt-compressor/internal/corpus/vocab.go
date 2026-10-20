package corpus

import "strings"

// The corpus describes one fictional company, Acme, and its "ledger"
// product: a Go API, a Python worker, a TypeScript web app, a Rust parser
// crate and a Java reporting service. Every name, host and address below is
// invented; hosts use example.com / example.internal and addresses use
// private or documentation ranges.

const (
	goModule   = "git.example.com/acme/ledger"
	repoHost   = "git.example.com"
	repoPath   = "acme/ledger"
	workDir    = "/home/dev/src/ledger"
	ciWorkDir  = "/builds/acme/ledger"
	javaGroup  = "com.example.acme.ledger"
	registry   = "registry.example.internal"
	pyPackage  = "ledger_worker"
	rustCrate  = "ledger-parser"
	tsApp      = "ledger-web"
	tfProject  = "acme-ledger-prod"
	exampleOrg = "acme"
)

var goPackages = []string{
	"internal/billing", "internal/store", "internal/api", "internal/auth",
	"internal/ratelimit", "internal/export", "internal/ledger", "internal/money",
	"internal/webhook", "internal/queue", "pkg/client", "pkg/currency",
}

var goTypes = []string{"Invoice", "Account", "Ledger", "Payment", "Refund", "Batch", "Entry", "Webhook", "Tenant", "Statement"}

var goFuncs = []string{
	"Total", "Apply", "Validate", "Reconcile", "Post", "Reverse", "Settle", "Export",
	"Lookup", "Flush", "Rollup", "Normalize", "Split", "Merge", "Close", "Open",
}

var goVerbs = []string{
	"RejectsNegativeAmount", "RoundsHalfEven", "HandlesEmptyBatch", "RetriesOnConflict",
	"KeepsInsertionOrder", "SkipsVoidedEntries", "ParsesISOCurrency", "ExpiresStaleLocks",
	"MergesDuplicateLines", "ComputesTaxPerLine", "RespectsTenantScope", "StreamsLargeExport",
	"ReturnsNotFound", "PaginatesByCursor", "IdempotentOnReplay", "ClosesPeriod",
}

var pyModules = []string{
	"tests/test_reconcile.py", "tests/test_export.py", "tests/test_currency.py",
	"tests/test_ingest.py", "tests/test_rollup.py", "tests/test_webhooks.py",
	"tests/integration/test_queue.py", "tests/integration/test_store.py",
	"tests/unit/test_parser.py", "tests/unit/test_rates.py",
}

var pyFuncs = []string{
	"test_reconcile_matches_by_reference", "test_export_csv_header", "test_rounding_half_even",
	"test_ingest_skips_duplicates", "test_rollup_by_day", "test_webhook_signature_header",
	"test_queue_visibility_timeout", "test_store_upsert_conflict", "test_parse_amount_locale",
	"test_rates_cache_expiry", "test_empty_batch_is_noop", "test_refund_exceeds_charge",
}

var tsFiles = []string{
	"src/components/InvoiceTable.test.tsx", "src/components/AccountPicker.test.tsx",
	"src/hooks/useLedger.test.ts", "src/utils/money.test.ts", "src/utils/dates.test.ts",
	"src/pages/Statements.test.tsx", "src/api/client.test.ts", "src/store/filters.test.ts",
	"src/components/RefundDialog.test.tsx", "src/components/Pagination.test.tsx",
}

var rustMods = []string{"parser", "lexer", "amount", "currency", "csv_reader", "ofx", "camt053", "mt940"}

var javaClasses = []string{
	"StatementService", "ReportGenerator", "LedgerRepository", "ExportController",
	"CurrencyConverter", "PeriodCloser", "AuditTrail", "TenantResolver",
}

var mavenArtifacts = []string{
	"org/springframework/boot/spring-boot-starter-web/3.3.4/spring-boot-starter-web-3.3.4",
	"org/springframework/spring-core/6.1.13/spring-core-6.1.13",
	"com/fasterxml/jackson/core/jackson-databind/2.17.2/jackson-databind-2.17.2",
	"org/apache/commons/commons-lang3/3.14.0/commons-lang3-3.14.0",
	"org/junit/jupiter/junit-jupiter-api/5.10.3/junit-jupiter-api-5.10.3",
	"org/mockito/mockito-core/5.12.0/mockito-core-5.12.0",
	"org/postgresql/postgresql/42.7.4/postgresql-42.7.4",
	"io/micrometer/micrometer-core/1.13.4/micrometer-core-1.13.4",
	"org/slf4j/slf4j-api/2.0.16/slf4j-api-2.0.16",
	"ch/qos/logback/logback-classic/1.5.8/logback-classic-1.5.8",
	"org/hibernate/orm/hibernate-core/6.5.3.Final/hibernate-core-6.5.3.Final",
	"com/google/guava/guava/33.3.0-jre/guava-33.3.0-jre",
	"org/apache/maven/plugins/maven-surefire-plugin/3.2.5/maven-surefire-plugin-3.2.5",
	"org/apache/maven/plugins/maven-compiler-plugin/3.13.0/maven-compiler-plugin-3.13.0",
	"org/yaml/snakeyaml/2.2/snakeyaml-2.2",
	"net/bytebuddy/byte-buddy/1.14.19/byte-buddy-1.14.19",
}

var crates = []string{
	"proc-macro2 v1.0.86", "unicode-ident v1.0.12", "quote v1.0.37", "syn v2.0.77",
	"serde v1.0.210", "serde_derive v1.0.210", "serde_json v1.0.128", "itoa v1.0.11",
	"memchr v2.7.4", "ryu v1.0.18", "thiserror v1.0.63", "thiserror-impl v1.0.63",
	"regex-syntax v0.8.4", "aho-corasick v1.1.3", "regex-automata v0.4.7", "regex v1.10.6",
	"rust_decimal v1.36.0", "chrono v0.4.38", "num-traits v0.2.19", "autocfg v1.3.0",
	"csv-core v0.1.11", "csv v1.3.0", "quick-xml v0.36.1", "once_cell v1.19.0",
	"libc v0.2.158", "cfg-if v1.0.0", "bytes v1.7.1", "smallvec v1.13.2",
}

var npmDeprecated = []string{
	"inflight@1.0.6: This module is not supported, and leaks memory. Do not use it.",
	"glob@7.2.3: Glob versions prior to v9 are no longer supported",
	"rimraf@3.0.2: Rimraf versions prior to v4 are no longer supported",
	"abab@2.0.6: Use your platform's native atob() and btoa() methods instead",
	"domexception@4.0.0: Use your platform's native DOMException instead",
	"@humanwhocodes/config-array@0.11.14: Use @eslint/config-array instead",
}

var debPackages = []string{
	"libssl3", "ca-certificates", "libc6", "libgcc-s1", "libstdc++6", "zlib1g", "libzstd1",
	"libcurl4", "curl", "git", "openssh-client", "tzdata", "libpq5", "libsqlite3-0",
	"libexpat1", "libffi8", "libncursesw6", "libreadline8", "netbase", "libgnutls30",
}

var k8sNamespaces = []string{"ledger-prod", "ledger-staging", "ingress-nginx", "kube-system", "monitoring", "cert-manager"}

var k8sApps = []string{"ledger-api", "ledger-worker", "ledger-web", "ledger-export", "ledger-scheduler", "pgbouncer", "redis", "otel-collector"}

var tfResources = []struct{ typ, name string }{
	{"aws_instance", "api"}, {"aws_security_group", "api"}, {"aws_lb_target_group", "api"},
	{"aws_db_instance", "ledger"}, {"aws_s3_bucket", "exports"}, {"aws_iam_role", "worker"},
	{"aws_iam_role_policy", "worker_s3"}, {"aws_cloudwatch_log_group", "api"},
	{"aws_sqs_queue", "webhooks"}, {"aws_elasticache_replication_group", "cache"},
	{"aws_route53_record", "api"}, {"aws_autoscaling_group", "worker"},
}

var dirNames = []string{"cmd", "internal", "pkg", "web", "worker", "parser", "reports", "deploy", "docs", "scripts", "testdata", "migrations"}

var fileExts = []string{".go", ".go", ".go", "_test.go", ".py", ".ts", ".tsx", ".rs", ".java", ".sql", ".yaml", ".md", ".json"}

var firstNames = []string{"alex", "sam", "jordan", "taylor", "morgan", "casey", "riley", "jamie", "devon", "quinn", "robin", "avery"}

var labels = []string{"bug", "enhancement", "billing", "export", "good first issue", "needs-triage", "p1", "p2", "regression", "docs", "performance", "security"}

var regions = []string{"eu-west-1", "us-east-1", "ap-southeast-2", "eu-central-1", "us-west-2"}

var skus = []string{"LED-STARTER", "LED-TEAM", "LED-BUSINESS", "LED-ENTERPRISE", "ADDON-SSO", "ADDON-AUDIT", "ADDON-EXPORT", "SUPPORT-PREMIUM"}

var currencies = []string{"EUR", "USD", "GBP", "AUD", "NZD", "SGD"}

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
