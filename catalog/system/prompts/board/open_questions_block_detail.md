---
key: board.open_questions_block_detail
version: 1
inputs: [Lines]
---

Blocking question(s) need an answer before this analysis can responsibly continue:
{{join "\n" .Lines}}
The human answers them on the report page; this run ends here and resumes once every one of them has an answer.
