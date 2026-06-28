package indir

import (
	"fmt"
	"strings"
	"time"
)

const (
	barWidth     = 28
	tickInterval = 200 * time.Millisecond
	emaAlpha     = 0.3 // exponential moving average factor for speed
)

// displayProgress renders an updating one-line progress bar to stdout.
// It returns after stop is closed.
func (d *Downloader) displayProgress(stop <-chan struct{}) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	var lastBytes int64
	lastTick := time.Now()
	var speed float64 // EMA of bytes/s

	render := func() {
		done := d.doneBytes.Load()
		total := d.totalBytes

		// Update EMA speed
		now := time.Now()
		if dt := now.Sub(lastTick).Seconds(); dt > 0 {
			inst := float64(done-lastBytes) / dt
			if speed == 0 {
				speed = inst
			} else {
				speed = (1-emaAlpha)*speed + emaAlpha*inst
			}
			lastBytes = done
			lastTick = now
		}

		if total > 0 {
			pct := float64(done) / float64(total) * 100
			if pct > 100 {
				pct = 100
			}

			filled := int(pct / 100 * barWidth)
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

			eta := "?"
			if speed > 0 && done < total {
				eta = formatDuration(float64(total-done) / speed)
			} else if done >= total {
				eta = "0s"
			}

			fmt.Printf("\r[%s] %5.1f%%  %s / %s  %-12s  ETA %s   ",
				bar, pct,
				formatSize(done), formatSize(total),
				formatSpeed(speed), eta,
			)
		} else {
			// Unknown total (streaming / no Content-Length)
			fmt.Printf("\r%s indirilen   %s   ", formatSize(done), formatSpeed(speed))
		}
	}

	for {
		select {
		case <-stop:
			render()
			fmt.Println()
			return
		case <-ticker.C:
			render()
		}
	}
}

// ── formatters ───────────────────────────────────────────────────────────────

func formatSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func formatSpeed(bps float64) string {
	switch {
	case bps <= 0:
		return "--"
	case bps >= 1<<30:
		return fmt.Sprintf("%.2f GB/s", bps/(1<<30))
	case bps >= 1<<20:
		return fmt.Sprintf("%.2f MB/s", bps/(1<<20))
	case bps >= 1<<10:
		return fmt.Sprintf("%.2f KB/s", bps/(1<<10))
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

func formatDuration(sec float64) string {
	if sec < 0 || sec > 86400 {
		return "?"
	}
	s := int(sec)
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm%ds", s/60, s%60)
	default:
		return fmt.Sprintf("%dh%dm", s/3600, (s%3600)/60)
	}
}
