package prompt

import (
	"context"
	"errors"
	"testing"
	"time"

	"charm.land/huh/v2"
)

func TestRunError(t *testing.T) {
	t.Parallel()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancelExpired()
	tests := map[string]struct {
		ctx  context.Context
		err  error
		want error
	}{
		"cancelled context hides huh's timeout": {ctx: cancelled, err: huh.ErrTimeout, want: context.Canceled},
		"expired context":                       {ctx: expired, err: huh.ErrTimeout, want: context.DeadlineExceeded},
		"ctrl-c":                                {ctx: context.Background(), err: huh.ErrUserAborted, want: ErrInterrupted},
		"other error":                           {ctx: context.Background(), err: huh.ErrTimeout, want: huh.ErrTimeout},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := runError(tt.ctx, tt.err)
			if !errors.Is(got, tt.want) {
				t.Fatalf("runError = %v, want cause %v", got, tt.want)
			}
		})
	}
}
