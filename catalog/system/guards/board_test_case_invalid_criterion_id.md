---
key: guard.board_test_case_invalid_criterion_id
version: 1
inputs: [Title, RawID]
---
case {{printf "%q" .Title}} has an invalid criterion_id {{printf "%q" .RawID}}; use an id from list_acceptance_criteria, or leave it empty
