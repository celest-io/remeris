package denolock

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// bigLockfile builds a canonical-looking lockfile with n jsr packages, n npm packages that depend on each other, and n
// remote entries: a few megabytes for n around 15 000.
func bigLockfile(n int) string {
	var b strings.Builder
	b.WriteString(`{"version":"5","specifiers":{`)
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"jsr:@scope/pkg%d@^1.%d.0":"1.%d.%d"`, i, i%50, i%50, i%7)
	}
	b.WriteString(`},"jsr":{`)
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		dep := (i + 1) % n
		fmt.Fprintf(&b, `"@scope/pkg%d@1.%d.%d":{"integrity":"sha256-%064d","dependencies":["jsr:@scope/pkg%d@^1.%d.0"]}`,
			i, i%50, i%7, i, dep, dep%50)
	}
	b.WriteString(`},"npm":{`)
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(
			&b,
			`"pkg-%d@1.0.%d":{"integrity":"sha512-%0100d","dependencies":["pkg-%d","alias%d@npm:pkg-%d@1.0.%d"],"os":["linux","darwin"]}`,
			i,
			i%9,
			i,
			(i+1)%n,
			i,
			(i+2)%n,
			(i+2)%n%9,
		)
	}
	b.WriteString(`},"remote":{`)
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"https://example.com/mod/%d/file.ts":"%064d"`, n-i, i)
	}
	b.WriteString(`},"workspace":{"dependencies":[`)
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"jsr:@scope/pkg%d@^1.%d.0"`, i, i%50)
	}
	b.WriteString(`]}}`)
	return b.String()
}

// timeRoundTrip returns the best of three runs of Parse and Print.
func timeRoundTrip(t *testing.T, src []byte) time.Duration {
	t.Helper()
	best := time.Duration(1 << 62)
	for range 3 {
		start := time.Now()
		c, err := Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		_ = c.Print()
		best = min(best, time.Since(start))
	}
	return best
}

// TestLargeFile reads and prints a file of several megabytes, and checks that the time grows linearly: four times
// the data must not take ten times as long (a quadratic algorithm takes sixteen times).
func TestLargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	small := []byte(bigLockfile(4000))
	large := []byte(bigLockfile(16000))
	if len(large) < 4<<20 {
		t.Fatalf("the large lockfile is only %d bytes", len(large))
	}
	c, err := Parse(large)
	if err != nil {
		t.Fatal(err)
	}
	first := c.Print()
	assertFixedPoint(t, first)

	tSmall, tLarge := timeRoundTrip(t, small), timeRoundTrip(t, large)
	t.Logf("%d bytes in %v, %d bytes in %v", len(small), tSmall, len(large), tLarge)
	if tLarge > 10*tSmall+100*time.Millisecond {
		t.Fatalf("not linear: %v for %d bytes, %v for %d bytes", tSmall, len(small), tLarge, len(large))
	}
	if tLarge > 20*time.Second {
		t.Fatalf("too slow: %v", tLarge)
	}
}

// TestManyKeysInOneObject guards the parser against the quadratic scan for repeated names.
func TestManyKeysInOneObject(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	var b strings.Builder
	b.WriteString(`{"remote":{`)
	for i := range 200000 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"https://example.com/%d":"x"`, i)
	}
	b.WriteString(`}}`)
	start := time.Now()
	c, err := Parse([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Remote) != 200000 {
		t.Fatalf("%d entries", len(c.Remote))
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("too slow: %v", d)
	}
}
