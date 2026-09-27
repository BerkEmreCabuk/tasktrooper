---
key: tool.set_test_case_result
version: "1"
params:
    actual: 'Required for failed: what you observed instead of the expectation.'
    evidence: The command and its output, the request/response, or the screenshot path that proves this verdict.
    notes: Required for skipped (what blocked it) and invalid (why it is not a valid case).
    status: passed | failed | skipped | invalid
    test_case_id: Test case UUID (from list_test_cases or record_test_cases)
---
Record the verdict on ONE test case you just executed: passed, failed (with what you actually observed), skipped (with what blocked it) or invalid (with why it is not a valid case). Fields you leave empty keep their stored value.
