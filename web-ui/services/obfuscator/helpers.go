package obfuscator

import "crypto/rand"

// randomRange returns a cryptographically random int in [lower, upper].
// Using crypto/rand ensures obfuscation masks are unpredictable across
// reboots and requests, regardless of Go version seeding behaviour.
func randomRange(lower, upper int) int {
	n := upper - lower + 1
	buf := make([]byte, 1)
	for {
		_, _ = rand.Read(buf)
		val := int(buf[0])
		// Reject values that would introduce modulo bias
		if val < (256/n)*n {
			return lower + val%n
		}
	}
}

func strShuffle(str string) string {
	inRune := []rune(str)
	// Fisher-Yates shuffle using crypto/rand
	for i := len(inRune) - 1; i > 0; i-- {
		j := randomRange(0, i)
		inRune[i], inRune[j] = inRune[j], inRune[i]
	}
	return string(inRune)
}
