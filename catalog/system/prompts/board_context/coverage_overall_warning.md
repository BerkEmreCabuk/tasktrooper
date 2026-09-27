---
key: board_context.coverage_overall_warning
version: 1
inputs: [Marker, Percent, Threshold]
---
{{.Marker}} overall {{.Percent}}% is below the {{.Threshold}}% this repository asks for.
What is missing is coverage of code this task did not touch, so treat it as a note for whoever reads this run: if untested paths sit next to your change — the branches that handle errors and edge cases — covering them is worth a few minutes. It does not hold the task: the hand-off proceeds either way.
