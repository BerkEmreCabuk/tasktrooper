package agent

import "testing"

// TestPromptGoldenBeforeMove pins the exact byte output of every LLM-facing
// string this package builds today (budget.go, clarification_gate.go,
// digest.go), before it moves into catalog/system/prompts/agent_loop/**. The
// move must keep every one of these assertions passing unchanged.
func TestPromptGoldenBeforeMove(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{
			"budgetWarningMessage/1",
			budgetWarningMessage(1),
			"[budget] 1 model turns left in this run. Stop exploring and stop re-reading files. Apply the smallest change that completes the task now, then reply with a plain-text summary of what you changed and what is left. Unfinished work is committed to the task branch and picked up by the next run, so a clear summary is more useful than a rushed edit.",
		},
		{
			"budgetWarningMessage/3",
			budgetWarningMessage(3),
			"[budget] 3 model turns left in this run. Stop exploring and stop re-reading files. Apply the smallest change that completes the task now, then reply with a plain-text summary of what you changed and what is left. Unfinished work is committed to the task branch and picked up by the next run, so a clear summary is more useful than a rushed edit.",
		},
		{
			"budgetWarningMessage/10",
			budgetWarningMessage(10),
			"[budget] 10 model turns left in this run. Stop exploring and stop re-reading files. Apply the smallest change that completes the task now, then reply with a plain-text summary of what you changed and what is left. Unfinished work is committed to the task branch and picked up by the next run, so a clear summary is more useful than a rushed edit.",
		},
		{
			"tokenBudgetWarningMessage/small",
			tokenBudgetWarningMessage(850, 1000),
			"[token budget] This run has used about 850 of its 1000 token budget. Stop exploring and stop re-reading files. Apply the smallest change that completes the task now, then reply with a plain-text summary of what you changed and what is left. Unfinished work is committed to the task branch and picked up by the next run, so a clear summary is more useful than a rushed edit.",
		},
		{
			"tokenBudgetWarningMessage/large",
			tokenBudgetWarningMessage(136000, 160000),
			"[token budget] This run has used about 136000 of its 160000 token budget. Stop exploring and stop re-reading files. Apply the smallest change that completes the task now, then reply with a plain-text summary of what you changed and what is left. Unfinished work is committed to the task branch and picked up by the next run, so a clear summary is more useful than a rushed edit.",
		},
		{
			"tokenBudgetWarningMessage/zero",
			tokenBudgetWarningMessage(0, 500),
			"[token budget] This run has used about 0 of its 500 token budget. Stop exploring and stop re-reading files. Apply the smallest change that completes the task now, then reply with a plain-text summary of what you changed and what is left. Unfinished work is committed to the task branch and picked up by the next run, so a clear summary is more useful than a rushed edit.",
		},
		{
			"repeatNudgeMessage/first",
			repeatNudgeMessage("grep_code", 1),
			"[loop guard] You have now made this exact grep_code call 2 times and it returned the same result every time. That result is no longer shown to you, and the call is no longer being run — asking again gains nothing.\nEmpty output is not a failure: sed, mv, cp and mkdir print nothing when they succeed.\nDo one of these instead:\n1. Read the file or state this call was meant to change, and continue from what you find.\n2. Change the method — write the whole file instead of editing it in place, or use a different tool.\n3. If neither is possible, stop calling tools and summarise what you changed and what is blocked.\n3 more identical calls and this run is stopped with the task unfinished.",
		},
		{
			"repeatNudgeMessage/lastWarning",
			repeatNudgeMessage("read_file", 3),
			"[loop guard] You have now made this exact read_file call 4 times and it returned the same result every time. That result is no longer shown to you, and the call is no longer being run — asking again gains nothing.\nEmpty output is not a failure: sed, mv, cp and mkdir print nothing when they succeed.\nDo one of these instead:\n1. Read the file or state this call was meant to change, and continue from what you find.\n2. Change the method — write the whole file instead of editing it in place, or use a different tool.\n3. If neither is possible, stop calling tools and summarise what you changed and what is blocked.\n1 more identical call and this run is stopped with the task unfinished.",
		},
		{
			"repeatNudgeMessage/pastAbort",
			repeatNudgeMessage("bash", 5),
			"[loop guard] You have now made this exact bash call 6 times and it returned the same result every time. That result is no longer shown to you, and the call is no longer being run — asking again gains nothing.\nEmpty output is not a failure: sed, mv, cp and mkdir print nothing when they succeed.\nDo one of these instead:\n1. Read the file or state this call was meant to change, and continue from what you find.\n2. Change the method — write the whole file instead of editing it in place, or use a different tool.\n3. If neither is possible, stop calling tools and summarise what you changed and what is blocked.\n1 more identical call and this run is stopped with the task unfinished.",
		},
		{
			"sameCallMessage/first",
			sameCallMessage("run_terminal", 3),
			"\n\n[loop guard] You have now run this exact run_terminal call 3 times in this run. Its result changes slightly each time (a duration, a timestamp, a counter on the page), but nothing about the task has changed with it.\nIf it passed, you already have your evidence — record it and move the task on. If it failed, the next call must be an EDIT that changes the cause; running the same command again cannot change the outcome.\nOnly call it again after you have changed something it would actually see. 5 more identical calls and this run is stopped with the task unfinished.",
		},
		{
			"sameCallMessage/lastWarning",
			sameCallMessage("run_terminal", 7),
			"\n\n[loop guard] You have now run this exact run_terminal call 7 times in this run. Its result changes slightly each time (a duration, a timestamp, a counter on the page), but nothing about the task has changed with it.\nIf it passed, you already have your evidence — record it and move the task on. If it failed, the next call must be an EDIT that changes the cause; running the same command again cannot change the outcome.\nOnly call it again after you have changed something it would actually see. 1 more identical call and this run is stopped with the task unfinished.",
		},
		{
			"sameCallMessage/pastAbort",
			sameCallMessage("run_terminal", 9),
			"\n\n[loop guard] You have now run this exact run_terminal call 9 times in this run. Its result changes slightly each time (a duration, a timestamp, a counter on the page), but nothing about the task has changed with it.\nIf it passed, you already have your evidence — record it and move the task on. If it failed, the next call must be an EDIT that changes the cause; running the same command again cannot change the outcome.\nOnly call it again after you have changed something it would actually see. 1 more identical call and this run is stopped with the task unfinished.",
		},
		{
			"emptyResultNote/grep",
			emptyResultNote("grep_code"),
			"[no output] grep_code ran successfully and returned nothing at all.\nThat empty result is the tool's answer, not a failure to run: whatever you asked for is not there, or the arguments pointed at something that holds nothing.\nRepeating this exact call will return the same emptiness. Change the arguments, or use a different tool.",
		},
		{
			"emptyResultNote/list",
			emptyResultNote("list_directory"),
			"[no output] list_directory ran successfully and returned nothing at all.\nThat empty result is the tool's answer, not a failure to run: whatever you asked for is not there, or the arguments pointed at something that holds nothing.\nRepeating this exact call will return the same emptiness. Change the arguments, or use a different tool.",
		},
		{
			"errorStreakMessage/first",
			errorStreakMessage(3),
			"[loop guard] Your last 3 tool calls in a row all failed. Changing only the arguments is not working; the method is what is wrong.\nBefore the next call, do one of these:\n1. Read the actual file, directory or command output the failures are about, instead of guessing at paths.\n2. Use a different tool for the same goal — write the whole file rather than patching it, list a directory rather than assuming it.\n3. If the environment is missing something you need, stop and summarise what is blocked instead of retrying.\n5 more consecutive failures and this run is stopped with the task unfinished.\n\nThe failing call's own output follows:\n",
		},
		{
			"errorStreakMessage/lastWarning",
			errorStreakMessage(7),
			"[loop guard] Your last 7 tool calls in a row all failed. Changing only the arguments is not working; the method is what is wrong.\nBefore the next call, do one of these:\n1. Read the actual file, directory or command output the failures are about, instead of guessing at paths.\n2. Use a different tool for the same goal — write the whole file rather than patching it, list a directory rather than assuming it.\n3. If the environment is missing something you need, stop and summarise what is blocked instead of retrying.\n1 more consecutive failure and this run is stopped with the task unfinished.\n\nThe failing call's own output follows:\n",
		},
		{
			"errorStreakMessage/pastAbort",
			errorStreakMessage(9),
			"[loop guard] Your last 9 tool calls in a row all failed. Changing only the arguments is not working; the method is what is wrong.\nBefore the next call, do one of these:\n1. Read the actual file, directory or command output the failures are about, instead of guessing at paths.\n2. Use a different tool for the same goal — write the whole file rather than patching it, list a directory rather than assuming it.\n3. If the environment is missing something you need, stop and summarise what is blocked instead of retrying.\n1 more consecutive failure and this run is stopped with the task unfinished.\n\nThe failing call's own output follows:\n",
		},
		{
			"toolErrorMessage/first",
			toolErrorMessage("write_file", 5),
			"\n\n[loop guard] write_file has now failed 5 times in this run. Whatever you are passing it is not the shape it wants — check its description again, or reach the same goal with a different tool.",
		},
		{
			"toolErrorMessage/later",
			toolErrorMessage("edit_lines", 7),
			"\n\n[loop guard] edit_lines has now failed 7 times in this run. Whatever you are passing it is not the shape it wants — check its description again, or reach the same goal with a different tool.",
		},
		{
			"wrapUpPrompt",
			wrapUpPrompt,
			"Your tool budget for this run is spent. Do not call any more tools. Reply with a short plain-text summary: what you changed (files), what works, and what is still missing.",
		},
		{
			"emptyTurnPrompt",
			emptyTurnPrompt,
			"Your last turn was empty — no text and no tool call. An empty turn is not an answer. Reply now, in plain text, to what was asked: what you did, what it changed, and what is left. If a tool call is still needed to answer, make it.",
		},
		{
			"emptyTurnFallback",
			emptyTurnFallback,
			"The model returned an empty answer twice in a row, so this turn produced no reply. Any tool calls made before it did run — check the board records and the actions listed above — but nothing was written back here. Send the request again, ideally in smaller steps.",
		},
		{
			"groundClarificationNote",
			groundClarificationNote,
			"ask_user rejected: this run has not read the repository yet, so it cannot know which of its questions the code already answers. Look first — codebase_search / grep_code / get_repo_tree / get_symbol_skeleton / expand_symbol_context work in the task workspace and answer anything about file layout, existing components, routing or configuration. Ask the human only about what the repository cannot contain: product decisions, priorities, external URLs, credentials, or which of several valid designs they want. Then call ask_user again if something is still unknown.",
		},
		{
			"FindingsDigestHeader",
			FindingsDigestHeader,
			"[findings] What the previous attempt already did — continue from it, do not rediscover it.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("mismatch:\n got:  %q\n want: %q", tc.got, tc.want)
			}
		})
	}
}
