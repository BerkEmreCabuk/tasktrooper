package github

func NewAPIErrorForTest(status int, message string) error {
	return &apiError{Status: status, Message: message}
}
