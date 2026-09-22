package llmprovider

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyProviderError(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		message string
		want    ProviderErrorKind
	}{
		{
			name:    "mistral reports a spent plan as 401",
			status:  http.StatusUnauthorized,
			message: "Inactive subscription or usage limit reached",
			want:    ProviderErrorQuotaExhausted,
		},
		{
			name:    "a plain 401 is a bad key",
			status:  http.StatusUnauthorized,
			message: "Unauthorized",
			want:    ProviderErrorAuthFailed,
		},
		{
			name:    "an empty 401 body is still a bad key",
			status:  http.StatusUnauthorized,
			message: "",
			want:    ProviderErrorAuthFailed,
		},
		{
			name:    "openai reports a spent plan as 429",
			status:  http.StatusTooManyRequests,
			message: "You exceeded your current quota, please check your plan and billing details",
			want:    ProviderErrorQuotaExhausted,
		},
		{
			name:    "a 429 about pacing is not a spent plan",
			status:  http.StatusTooManyRequests,
			message: "Rate limit exceeded, please retry in 1s",
			want:    ProviderErrorRateLimited,
		},
		{
			name:    "a bare 429 is pacing",
			status:  http.StatusTooManyRequests,
			message: "",
			want:    ProviderErrorRateLimited,
		},
		{
			name:    "402 needs no message",
			status:  http.StatusPaymentRequired,
			message: "",
			want:    ProviderErrorQuotaExhausted,
		},
		{
			name:    "anthropic reports a spent balance as 400",
			status:  http.StatusBadRequest,
			message: "Your credit balance is too low to access the Claude API",
			want:    ProviderErrorQuotaExhausted,
		},
		{
			name:    "a 403 with no quota wording is a rejected key",
			status:  http.StatusForbidden,
			message: "Forbidden",
			want:    ProviderErrorAuthFailed,
		},
		{
			name:    "a server-side failure is left unclassified",
			status:  http.StatusBadGateway,
			message: "upstream connect error",
			want:    ProviderErrorUnknown,
		},
		{
			name:    "a 404 says nothing about quota or keys",
			status:  http.StatusNotFound,
			message: "not found",
			want:    ProviderErrorUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, classifyProviderError(tc.status, tc.message))
		})
	}
}

func TestProviderErrorMessageFormatting(t *testing.T) {
	withMessage := newProviderError("/models", http.StatusUnauthorized, "Inactive subscription")
	require.Equal(t, "/models returned status 401: Inactive subscription", withMessage.Error())

	bare := newProviderError("/models", http.StatusUnauthorized, "")
	require.Equal(t, "/models returned status 401", bare.Error())
}

func TestProviderErrorMessageTruncatesAndCollapsesWhitespace(t *testing.T) {
	long := make([]byte, 0, 1000)
	for i := 0; i < 500; i++ {
		long = append(long, 'a', ' ')
	}
	got := providerErrorMessage(long)
	require.Len(t, got, providerErrorMaxLen+len("…"))
	require.NotContains(t, got, "  ")
}
