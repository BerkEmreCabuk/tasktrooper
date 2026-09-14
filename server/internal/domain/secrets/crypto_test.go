package secrets

import (
	"os"
	"testing"

	"github.com/stretchr/testify/suite"
)

type CipherSuite struct {
	suite.Suite
	prevMCPKey    string
	prevServerKey string
}

func (s *CipherSuite) SetupTest() {
	s.prevMCPKey = os.Getenv("MCP_SECRETS_KEY")
	s.prevServerKey = os.Getenv("SERVER_API_KEY")
	os.Setenv("MCP_SECRETS_KEY", "test-mcp-secrets-key")
	os.Unsetenv("SERVER_API_KEY")
}

func (s *CipherSuite) TearDownTest() {
	if s.prevMCPKey == "" {
		os.Unsetenv("MCP_SECRETS_KEY")
	} else {
		os.Setenv("MCP_SECRETS_KEY", s.prevMCPKey)
	}
	if s.prevServerKey == "" {
		os.Unsetenv("SERVER_API_KEY")
	} else {
		os.Setenv("SERVER_API_KEY", s.prevServerKey)
	}
}

func (s *CipherSuite) TestEncryptDecryptRoundTrip() {
	cipher, err := NewCipherFromEnv()
	s.Require().NoError(err)

	encrypted, err := cipher.Encrypt("super-secret-token")
	s.Require().NoError(err)
	s.NotEmpty(encrypted)

	plain, err := cipher.Decrypt(encrypted)
	s.Require().NoError(err)
	s.Equal("super-secret-token", plain)
}

func (s *CipherSuite) TestDeriveFromServerAPIKey() {
	os.Unsetenv("MCP_SECRETS_KEY")
	os.Setenv("SERVER_API_KEY", "bridge-key")

	cipher, err := NewCipherFromEnv()
	s.Require().NoError(err)

	encrypted, err := cipher.Encrypt("value")
	s.Require().NoError(err)

	plain, err := cipher.Decrypt(encrypted)
	s.Require().NoError(err)
	s.Equal("value", plain)
}

func (s *CipherSuite) TestMissingKeyFails() {
	os.Unsetenv("MCP_SECRETS_KEY")
	os.Unsetenv("SERVER_API_KEY")

	_, err := NewCipherFromEnv()
	s.Error(err)
}

func TestCipherSuite(t *testing.T) {
	suite.Run(t, new(CipherSuite))
}
