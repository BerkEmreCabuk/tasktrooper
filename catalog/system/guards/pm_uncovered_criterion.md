---
key: guard.pm_uncovered_criterion
version: 1
---
pm_uat rejected: this run approved a criterion QA's own recorded test round never proves (no passed TaskTestCase links to it) without checking it yourself (no browser_* or mobile_* call succeeded). Trusting QA's review_criterion note is not enough when nothing in list_test_cases actually backs it — walk that criterion's flow yourself before approving it.
