---
key: agent_loop.empty_result_note
version: 1
inputs: [Name]
---
[no output] {{.Name}} ran successfully and returned nothing at all.
That empty result is the tool's answer, not a failure to run: whatever you asked for is not there, or the arguments pointed at something that holds nothing.
Repeating this exact call will return the same emptiness. Change the arguments, or use a different tool.
