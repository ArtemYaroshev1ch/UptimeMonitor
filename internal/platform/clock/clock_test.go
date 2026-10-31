package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFake(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := NewFake(start)
	assert.Equal(t, start, c.Now())

	c.Advance(time.Minute)
	assert.Equal(t, start.Add(time.Minute), c.Now())
}
