package prompt

// The file-editing tools (edit_file, edit_lines, delete_file, move_file,
// read_file, write_file — adapter/tools/code) collapse to a handful of
// refusal and success-note shapes; kept here so an LLM sees the same wording
// whichever tool hit the same condition.

var codeEditMissingOldStringKey = Define[struct{}]("guard.code_edit_missing_old_string", struct{}{})

// CodeEditMissingOldStringText is edit_file's refusal for an empty
// old_string.
func CodeEditMissingOldStringText() string { return Text(codeEditMissingOldStringKey) }

type codeEditNoSuchFileInput struct{ Path string }

var codeEditNoSuchFileKey = Define("guard.code_edit_no_such_file", codeEditNoSuchFileInput{Path: "src/app.ts"})

// CodeEditNoSuchFileText is edit_file's refusal for a path that does not
// exist.
func CodeEditNoSuchFileText(path string) string {
	return codeEditNoSuchFileKey.Render(codeEditNoSuchFileInput{Path: path})
}

type codeEditNotFoundInput struct{ Path string }

var codeEditNotFoundKey = Define("guard.code_edit_not_found", codeEditNotFoundInput{Path: "src/app.ts"})

// CodeEditNotFoundText is edit_file's refusal when old_string does not
// appear in the file.
func CodeEditNotFoundText(path string) string {
	return codeEditNotFoundKey.Render(codeEditNotFoundInput{Path: path})
}

type codeEditMultipleMatchesInput struct {
	Count int
	Path  string
}

var codeEditMultipleMatchesKey = Define("guard.code_edit_multiple_matches", codeEditMultipleMatchesInput{Count: 3, Path: "src/app.ts"})

// CodeEditMultipleMatchesText is edit_file's refusal when old_string is
// ambiguous and replace_all was not set.
func CodeEditMultipleMatchesText(count int, path string) string {
	return codeEditMultipleMatchesKey.Render(codeEditMultipleMatchesInput{Count: count, Path: path})
}

var codeEditDiskNoteKey = Define[struct{}]("tool_results.code_edit_disk_note", struct{}{})

// CodeEditDiskNoteText is appended to a successful edit_file result.
func CodeEditDiskNoteText() string { return Text(codeEditDiskNoteKey) }

type codeEditLinesUnknownModeInput struct{ Mode string }

var codeEditLinesUnknownModeKey = Define("guard.code_edit_lines_unknown_mode", codeEditLinesUnknownModeInput{Mode: `"append"`})

// CodeEditLinesUnknownModeText is edit_lines' refusal for a mode outside
// replace/insert_after/delete. mode must already be %q-quoted.
func CodeEditLinesUnknownModeText(mode string) string {
	return codeEditLinesUnknownModeKey.Render(codeEditLinesUnknownModeInput{Mode: mode})
}

var codeEditLinesTextRequiredKey = Define[struct{}]("guard.code_edit_lines_text_required", struct{}{})

// CodeEditLinesTextRequiredText is edit_lines' refusal for mode:replace with
// no text.
func CodeEditLinesTextRequiredText() string { return Text(codeEditLinesTextRequiredKey) }

type codeEditLinesNoSuchFileInput struct{ Path string }

var codeEditLinesNoSuchFileKey = Define("guard.code_edit_lines_no_such_file", codeEditLinesNoSuchFileInput{Path: "src/app.ts"})

// CodeEditLinesNoSuchFileText is edit_lines' refusal for a path that does
// not exist.
func CodeEditLinesNoSuchFileText(path string) string {
	return codeEditLinesNoSuchFileKey.Render(codeEditLinesNoSuchFileInput{Path: path})
}

var codeEditLinesDiskNoteKey = Define[struct{}]("tool_results.code_edit_lines_disk_note", struct{}{})

// CodeEditLinesDiskNoteText is appended to a successful edit_lines result.
func CodeEditLinesDiskNoteText() string { return Text(codeEditLinesDiskNoteKey) }

type codeDeleteNoSuchPathInput struct{ Path string }

var codeDeleteNoSuchPathKey = Define("guard.code_delete_no_such_path", codeDeleteNoSuchPathInput{Path: "src/app.ts"})

// CodeDeleteNoSuchPathText is delete_file's refusal for a path that does not
// exist.
func CodeDeleteNoSuchPathText(path string) string {
	return codeDeleteNoSuchPathKey.Render(codeDeleteNoSuchPathInput{Path: path})
}

type codeDeleteNeedsRecursiveInput struct {
	Path  string
	Count int
}

var codeDeleteNeedsRecursiveKey = Define("guard.code_delete_needs_recursive", codeDeleteNeedsRecursiveInput{Path: "pkg", Count: 3})

// CodeDeleteNeedsRecursiveText is delete_file's refusal for a non-empty
// directory without recursive:true.
func CodeDeleteNeedsRecursiveText(path string, count int) string {
	return codeDeleteNeedsRecursiveKey.Render(codeDeleteNeedsRecursiveInput{Path: path, Count: count})
}

var codeDeleteDiskNoteKey = Define[struct{}]("tool_results.code_delete_disk_note", struct{}{})

// CodeDeleteDiskNoteText is appended to a successful delete_file result.
func CodeDeleteDiskNoteText() string { return Text(codeDeleteDiskNoteKey) }

type codeMoveNoSuchPathInput struct{ From string }

var codeMoveNoSuchPathKey = Define("guard.code_move_no_such_path", codeMoveNoSuchPathInput{From: "old.ts"})

// CodeMoveNoSuchPathText is move_file's refusal when from does not exist.
func CodeMoveNoSuchPathText(from string) string {
	return codeMoveNoSuchPathKey.Render(codeMoveNoSuchPathInput{From: from})
}

type codeMoveDestExistsInput struct{ To string }

var codeMoveDestExistsKey = Define("guard.code_move_dest_exists", codeMoveDestExistsInput{To: "new.ts"})

// CodeMoveDestExistsText is move_file's refusal when to exists and
// overwrite was not set.
func CodeMoveDestExistsText(to string) string {
	return codeMoveDestExistsKey.Render(codeMoveDestExistsInput{To: to})
}

type codeMoveSuccessInput struct{ From, To string }

var codeMoveSuccessKey = Define("tool_results.code_move_success", codeMoveSuccessInput{From: "old.ts", To: "new.ts"})

// CodeMoveSuccessText is move_file's successful result.
func CodeMoveSuccessText(from, to string) string {
	return codeMoveSuccessKey.Render(codeMoveSuccessInput{From: from, To: to})
}

type codeReadNoSuchFileInput struct{ Path string }

var codeReadNoSuchFileKey = Define("guard.code_read_no_such_file", codeReadNoSuchFileInput{Path: "src/app.ts"})

// CodeReadNoSuchFileText is read_file's refusal for a path that does not
// exist.
func CodeReadNoSuchFileText(path string) string {
	return codeReadNoSuchFileKey.Render(codeReadNoSuchFileInput{Path: path})
}

type codeReadIsDirectoryInput struct{ Path string }

var codeReadIsDirectoryKey = Define("guard.code_read_is_directory", codeReadIsDirectoryInput{Path: "pkg"})

// CodeReadIsDirectoryText is read_file's refusal for a directory path.
func CodeReadIsDirectoryText(path string) string {
	return codeReadIsDirectoryKey.Render(codeReadIsDirectoryInput{Path: path})
}

type codeReadTooLargeInput struct {
	Path  string
	Bytes int64
}

var codeReadTooLargeKey = Define("guard.code_read_too_large", codeReadTooLargeInput{Path: "bundle.js", Bytes: 25000000})

// CodeReadTooLargeText is read_file's refusal for a file over
// maxReadFileBytes.
func CodeReadTooLargeText(path string, bytes int64) string {
	return codeReadTooLargeKey.Render(codeReadTooLargeInput{Path: path, Bytes: bytes})
}

type codeReadTruncatedInput struct {
	Last       int
	NextOffset int
}

var codeReadTruncatedKey = Define("tool_results.code_read_truncated", codeReadTruncatedInput{Last: 800, NextOffset: 801})

// CodeReadTruncatedText is read_file's footer when the char budget cut the
// window short of the requested limit.
func CodeReadTruncatedText(last, nextOffset int) string {
	return codeReadTruncatedKey.Render(codeReadTruncatedInput{Last: last, NextOffset: nextOffset})
}

type codeReadContinueInput struct {
	Remaining  int
	NextOffset int
}

var codeReadContinueKey = Define("tool_results.code_read_continue", codeReadContinueInput{Remaining: 400, NextOffset: 801})

// CodeReadContinueText is read_file's footer when more lines remain after
// the window it returned.
func CodeReadContinueText(remaining, nextOffset int) string {
	return codeReadContinueKey.Render(codeReadContinueInput{Remaining: remaining, NextOffset: nextOffset})
}

var codeWriteDiskNoteKey = Define[struct{}]("tool_results.code_write_disk_note", struct{}{})

// CodeWriteDiskNoteText is appended to a successful write_file result.
func CodeWriteDiskNoteText() string { return Text(codeWriteDiskNoteKey) }
