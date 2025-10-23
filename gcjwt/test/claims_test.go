package gcjwt_test

import (
	"io"
	"testing"
	"time"

	"github.com/bionicotaku/lingo-utils/gcjwt"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/stretchr/testify/require"
)

func TestCloudRunClaimsValidate(t *testing.T) {
	t.Parallel()

	now := time.Now()
	valid := gcjwt.CloudRunClaims{
		Audience:  "https://service-b.run.app/",
		Email:     "service-a@project.iam.gserviceaccount.com",
		ExpiresAt: now.Add(1 * time.Hour).Unix(),
	}

	t.Run("valid", func(t *testing.T) {
		require.NoError(t, valid.Validate(valid.Audience))
	})

	t.Run("audience mismatch", func(t *testing.T) {
		err := valid.Validate("https://another.run.app/")
		require.ErrorIs(t, err, gcjwt.ErrInvalidAudience)
	})

	t.Run("expired", func(t *testing.T) {
		expired := valid
		expired.ExpiresAt = now.Add(-time.Minute).Unix()
		err := expired.Validate(expired.Audience)
		require.Error(t, err)
		require.ErrorIs(t, err, gcjwt.ErrTokenExpired)
	})

	t.Run("missing email", func(t *testing.T) {
		missingEmail := valid
		missingEmail.Email = ""
		err := missingEmail.Validate(missingEmail.Audience)
		require.ErrorIs(t, err, gcjwt.ErrMissingEmail)
	})
}

func TestCloudRunClaimsValidateWithLogging(t *testing.T) {
	t.Parallel()

	logger := log.NewHelper(log.NewStdLogger(io.Discard))
	claims := gcjwt.CloudRunClaims{
		Audience:  "https://service-b.run.app/",
		Email:     "service-a@project.iam.gserviceaccount.com",
		ExpiresAt: time.Now().Add(2 * time.Hour).Unix(),
	}

	require.NoError(t, claims.ValidateWithLogging(claims.Audience, logger))

	err := claims.ValidateWithLogging("https://other.run.app/", logger)
	require.ErrorIs(t, err, gcjwt.ErrInvalidAudience)
}
