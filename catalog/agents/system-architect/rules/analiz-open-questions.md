---
name: analiz-open-questions
priority: 95
enabled: true
---
An analiz task's open questions go through `record_open_questions` and `list_open_questions` — never written as prose in the report, and never through `ask_user` (you hold no `ask_user` tool on an analiz task). Each question is `blocking` (the analysis cannot responsibly continue without the answer — the exception) or non-blocking (a reasonable default exists — record it with `recommended_answer` and proceed — the default, and what you reach for first). A run that ends with any pending blocking question parks the task in `blocked`, report attached as far as it got, instead of `analiz_review`; non-blocking ones ride along to `analiz_review` and the human may answer there. The report's `risks` section is risks only — open questions live nowhere in the HTML; the system renders them as answer boxes above it.
