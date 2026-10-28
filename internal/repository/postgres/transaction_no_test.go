package postgres

import (
	"regexp"
	"sync"
	"testing"
	"time"
)

// 批量发放会并发写各账本流水，流水号必须在高并发下不重复（曾撞 uq_integral_transactions_no）。
func TestGenerateTransactionNoConcurrentUnique(t *testing.T) {
	const (
		workers   = 64
		perWorker = 4000
	)
	results := make([][]string, workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out := make([]string, perWorker)
			for i := range out {
				out[i] = generateTransactionNo("INT")
			}
			results[w] = out
		}()
	}
	wg.Wait()

	seen := make(map[string]struct{}, workers*perWorker)
	for _, batch := range results {
		for _, no := range batch {
			if _, dup := seen[no]; dup {
				t.Fatalf("duplicate transaction no %s", no)
			}
			seen[no] = struct{}{}
		}
	}
}

func TestGenerateTransactionNoFormat(t *testing.T) {
	pattern := regexp.MustCompile(`^VIP(\d{14})(\d{6})$`)
	no := generateTransactionNo("VIP")
	m := pattern.FindStringSubmatch(no)
	if m == nil {
		t.Fatalf("unexpected format %q", no)
	}
	stamp, err := time.Parse("20060102150405", m[1])
	if err != nil {
		t.Fatalf("time part %q: %v", m[1], err)
	}
	if d := time.Since(stamp); d < -time.Second || d > time.Minute {
		t.Fatalf("time part %q is not current UTC", m[1])
	}
}

func TestRandomDigits(t *testing.T) {
	digits := regexp.MustCompile(`^\d{16}$`)
	seen := make(map[string]struct{})
	for range 1000 {
		v := randomDigits(16)
		if !digits.MatchString(v) {
			t.Fatalf("unexpected value %q", v)
		}
		seen[v] = struct{}{}
	}
	if len(seen) < 1000 {
		t.Fatalf("randomDigits repeated within 1000 draws: %d distinct", len(seen))
	}
}
