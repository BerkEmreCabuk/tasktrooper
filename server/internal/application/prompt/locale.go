package prompt

func LocaleDisplayName(lang string) string {
	switch lang {
	case "en":
		return "English"
	case "tr":
		return "Turkish"
	case "":
		return "English"
	default:
		return lang
	}
}
