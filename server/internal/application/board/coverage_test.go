package board

import (
	"math"
	"testing"
)

func floatPtr(f float64) *float64 { return &f }

func TestParseCoverage(t *testing.T) {
	tests := []struct {
		name string
		log  string
		want *float64
	}{
		{
			name: "go tool cover -func total line",
			log: `github.com/foo/bar/baz.go:10:  Foo             100.0%
github.com/foo/bar/baz.go:20:  Bar             66.7%
total:						(statements)		83.3%
`,
			want: floatPtr(83.3),
		},
		{
			name: "go test -cover per-package lines averaged",
			log: `ok      github.com/foo/bar/pkg1        0.012s  coverage: 80.0% of statements
ok      github.com/foo/bar/pkg2        0.008s  coverage: 90.0% of statements
`,
			want: floatPtr(85.0),
		},
		{
			name: "go test -cover single package",
			log:  `ok      github.com/foo/bar/pkg1        0.012s  coverage: 72.5% of statements`,
			want: floatPtr(72.5),
		},
		{
			name: "jest/vitest istanbul text-summary Lines line",
			log: `Test Suites: 5 passed, 5 total
Tests:       42 passed, 42 total
Lines        : 93.02% ( 120/129 )
Statements   : 92.31% ( 132/143 )
Branches     : 85.71% ( 12/14 )
Functions    : 90% ( 9/10 )
`,
			want: floatPtr(93.02),
		},
		{
			name: "jest/vitest istanbul text table form, last column is lines",
			log: `----------|---------|----------|---------|---------|-------------------
File      | % Stmts | % Branch | % Funcs | % Lines | Uncovered Line #s
----------|---------|----------|---------|---------|-------------------
All files |   92.31 |    85.71 |      90 |   93.02 |
foo.ts    |     100 |      100 |     100 |     100 |
----------|---------|----------|---------|---------|-------------------
`,
			want: floatPtr(93.02),
		},
		{
			name: "pytest-cov terminal TOTAL line",
			log: `Name                 Stmts   Miss  Cover
----------------------------------------
src/foo.py              50      5    90%
src/bar.py              30      3    90%
----------------------------------------
TOTAL                    80      8    90%
`,
			want: floatPtr(90.0),
		},
		{
			name: "generic fallback overall percentage",
			log: `Coverage Report
===============
Overall coverage: 76.4%
`,
			want: floatPtr(76.4),
		},
		{
			name: "generic fallback total percentage",
			log:  `Total coverage: 54.2%`,
			want: floatPtr(54.2),
		},
		{
			name: "value over 100 is rejected",
			log:  `Total memory used: 150.0%`,
			want: nil,
		},
		{
			name: "no coverage info at all",
			log: `=== RUN   TestFoo
--- PASS: TestFoo (0.00s)
PASS
ok  	github.com/foo/bar	0.010s
`,
			want: nil,
		},
		{
			name: "empty log",
			log:  ``,
			want: nil,
		},
		{
			name: "ANSI escape codes are stripped before matching",
			log:  "\x1b[32mLines\x1b[0m        : 88.8% ( 100/112 )\n",
			want: floatPtr(88.8),
		},
		{
			name: "go total line wins priority over generic overall mention elsewhere",
			log: `Overall this run looks fine.
total:						(statements)		77.7%
`,
			want: floatPtr(77.7),
		},
		{
			name: "go per-package coverage wins priority over generic fallback",
			log: `Summary: overall build succeeded.
ok      github.com/foo/bar/pkg1        0.012s  coverage: 64.0% of statements
`,
			want: floatPtr(64.0),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseCoverage(tc.log)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("ParseCoverage() = %v, want nil", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParseCoverage() = nil, want %v", *tc.want)
			}
			if math.Abs(*got-*tc.want) > 0.001 {
				t.Fatalf("ParseCoverage() = %v, want %v", *got, *tc.want)
			}
		})
	}
}
