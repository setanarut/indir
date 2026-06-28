// package indir provides multi-segment parallel HTTP file downloading
// with resume support, modelled after aria2c's -x and -s flags.
//
// Example usage:
//
//	d := downloader.New(downloader.Config{
//	    URL:            "https://example.com/large-file.iso",
//	    OutputPath:     "large-file.iso",
//	    MaxConnections: 4,  // aria2c -x 4
//	    Segments:       8,  // aria2c -s 8
//	})
//	if err := d.Download(); err != nil {
//	    log.Fatal(err)
//	}
//
// Ctrl-C saves progress; running again resumes from where it left off.
package indir

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
)

// Config holds options for a single download.
type Config struct {
	// URL is the remote resource to download.
	URL string

	// OutputPath is the local file that will be written when the download completes.
	OutputPath string

	// MaxConnections is the maximum number of concurrent connections to the server
	// (equivalent to aria2c -x). Default: 5.
	MaxConnections int

	// Segments is the number of pieces to split the file into
	// (equivalent to aria2c -s). Default: 5.
	Segments int
}

// Downloader downloads a file in parallel segments with resume support.
// Create one with New; call Download to run it.
type Downloader struct {
	cfg        Config
	client     *http.Client
	totalBytes int64
	doneBytes  atomic.Int64
	segments   []*segment
	statePath  string // path to the JSON resume-state file
}

// New returns a ready-to-use Downloader for the given Config.
// Zero/negative MaxConnections and Segments are replaced with 5.
func New(cfg Config) *Downloader {
	if cfg.MaxConnections <= 0 {
		cfg.MaxConnections = 5
	}
	if cfg.Segments <= 0 {
		cfg.Segments = 5
	}

	dir := filepath.Dir(cfg.OutputPath)
	if dir == "" {
		dir = "."
	}
	base := filepath.Base(cfg.OutputPath)

	return &Downloader{
		cfg:       cfg,
		client:    &http.Client{}, // no global timeout — large files need time
		statePath: filepath.Join(dir, "."+base+".dstate"),
	}
}

// Download starts or resumes the file download. It blocks until:
//   - the download completes (returns nil),
//   - SIGINT/SIGTERM is received (saves progress, returns nil),
//   - or an unrecoverable error occurs (returns non-nil).
func (d *Downloader) Download() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── graceful shutdown on SIGINT / SIGTERM ──────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			fmt.Print("\n\nKesme sinyali alındı, durum kaydediliyor...\n")
			cancel()
		case <-ctx.Done():
		}
	}()

	// ── prepare segments (resume or fresh) ────────────────────────────────
	if err := d.initSegments(); err != nil {
		return err
	}

	// Sync downloaded-byte counters from actual temp files on disk.
	// This is the ground truth; it corrects stale state-file values.
	d.syncFromDisk()

	// ── live progress display ──────────────────────────────────────────────
	stopProgress := make(chan struct{})
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		d.displayProgress(stopProgress)
	}()

	dlErr := d.runDownload(ctx)

	close(stopProgress)
	<-progressDone // wait for the final progress line to be printed

	// ── interrupted? save state and exit cleanly ───────────────────────────
	if ctx.Err() != nil {
		if err := d.saveState(); err != nil {
			fmt.Printf("Uyarı: durum kaydedilemedi: %v\n", err)
		} else {
			fmt.Printf("Durum kaydedildi: %s\n", d.statePath)
			fmt.Println("Devam etmek için aynı komutu tekrar çalıştırın.")
		}
		return nil
	}

	if dlErr != nil {
		return dlErr
	}

	// ── all segments done — assemble final file ────────────────────────────
	fmt.Print("Parçalar birleştiriliyor... ")
	if err := d.merge(); err != nil {
		return fmt.Errorf("birleştirme hatası: %w", err)
	}
	os.Remove(d.statePath)
	fmt.Printf("✓  →  %s\n", d.cfg.OutputPath)
	return nil
}

// ── internal helpers ─────────────────────────────────────────────────────────

// initSegments loads a saved state file or issues a HEAD request to plan a
// fresh download.
func (d *Downloader) initSegments() error {
	// Try resume first
	if state, err := loadState(d.statePath); err == nil && state.URL == d.cfg.URL {
		d.totalBytes = state.TotalSize
		d.segments = state.toSegments()
		fmt.Printf("Kaldığı yerden devam: %s  (%s)\n",
			d.cfg.OutputPath, formatSize(d.totalBytes))
		return nil
	}

	// Fresh download — probe the server
	size, rangeOK, err := d.head()
	if err != nil {
		return fmt.Errorf("sunucuya bağlanılamadı: %w", err)
	}
	d.totalBytes = size

	n := d.cfg.Segments
	switch {
	case !rangeOK || size <= 0:
		// Server doesn't advertise range support or Content-Length is unknown.
		// Fall back to a single connection.
		n = 1
		d.segments = []*segment{{Index: 0, Start: 0, End: -1}}
	default:
		if int64(n) > size {
			n = int(size)
		}
		d.segments = splitSegments(size, n)
	}

	conc := min(n, d.cfg.MaxConnections)
	fmt.Printf("İndiriliyor       : %s\n", d.cfg.URL)
	fmt.Printf("Çıktı             : %s\n", d.cfg.OutputPath)
	fmt.Printf("Boyut             : %s\n", formatSize(size))
	fmt.Printf("Segment / Bağlantı: %d / %d\n\n", n, conc)
	return nil
}

// syncFromDisk reads each segment's temp-file size from disk and stores the
// cumulative total in doneBytes. Missing files reset the segment to zero.
func (d *Downloader) syncFromDisk() {
	var total int64
	for _, seg := range d.segments {
		seg.Downloaded = 0 // always reset; disk is ground truth
		if fi, err := os.Stat(d.tempPath(seg.Index)); err == nil {
			seg.Downloaded = fi.Size()
		}
		total += seg.Downloaded
	}
	d.doneBytes.Store(total)
}

// head performs a HEAD request and returns (Content-Length, Accept-Ranges=bytes, error).
func (d *Downloader) head() (size int64, rangeOK bool, err error) {
	resp, err := d.client.Head(d.cfg.URL)
	if err != nil {
		return 0, false, err
	}
	resp.Body.Close()
	return resp.ContentLength, resp.Header.Get("Accept-Ranges") == "bytes", nil
}

// runDownload fires goroutines for every pending segment, bounded by
// MaxConnections using a semaphore channel.
func (d *Downloader) runDownload(ctx context.Context) error {
	conc := min(len(d.segments), d.cfg.MaxConnections)
	sem := make(chan struct{}, conc)

	var wg sync.WaitGroup
	errCh := make(chan error, len(d.segments))

	for _, seg := range d.segments {
		if seg.isDone() {
			continue
		}
		wg.Add(1)
		go func(s *segment) {
			defer wg.Done()

			// Acquire a concurrency slot or bail on cancellation.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}
			if err := d.downloadSegment(ctx, s); err != nil && ctx.Err() == nil {
				errCh <- fmt.Errorf("segment %d: %w", s.Index, err)
			}
		}(seg)
	}

	wg.Wait()
	close(errCh)
	return <-errCh // first error, or nil
}

// downloadSegment fetches one byte-range and appends it to the segment's
// temp file, updating doneBytes atomically after each read.
func (d *Downloader) downloadSegment(ctx context.Context, seg *segment) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.cfg.URL, nil)
	if err != nil {
		return err
	}

	// Build the Range header so we start exactly where we left off.
	from := seg.Start + seg.Downloaded
	switch {
	case seg.End >= 0:
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, seg.End))
	case seg.Downloaded > 0:
		// Streaming / unknown length but partial progress exists.
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", from))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("sunucu yanıtı: HTTP %d", resp.StatusCode)
	}

	// Open the temp file: append if resuming, truncate if starting fresh.
	flags := os.O_CREATE | os.O_WRONLY
	if seg.Downloaded > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(d.tempPath(seg.Index), flags, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 32*1024) // 32 KB read buffer
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			seg.Downloaded += int64(n)
			d.doneBytes.Add(int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return nil
}

// merge concatenates all segment temp files (in order) into the final output
// file, then removes the temp files.
func (d *Downloader) merge() error {
	out, err := os.Create(d.cfg.OutputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	buf := make([]byte, 1<<20) // 1 MB copy buffer
	for _, seg := range d.segments {
		tp := d.tempPath(seg.Index)
		f, err := os.Open(tp)
		if err != nil {
			return fmt.Errorf("segment %d temp dosyası açılamadı: %w", seg.Index, err)
		}
		_, err = io.CopyBuffer(out, f, buf)
		f.Close()
		if err != nil {
			return err
		}
		os.Remove(tp)
	}
	return nil
}

// saveState persists current segment progress to the state file.
func (d *Downloader) saveState() error {
	segs := make([]segmentState, len(d.segments))
	for i, s := range d.segments {
		segs[i] = segmentState{
			Index:      s.Index,
			Start:      s.Start,
			End:        s.End,
			Downloaded: s.Downloaded,
		}
	}
	return persistState(d.statePath, &downloadState{
		URL:       d.cfg.URL,
		TotalSize: d.totalBytes,
		Segments:  segs,
	})
}

// tempPath returns the path for segment i's temporary download file.
func (d *Downloader) tempPath(i int) string {
	dir := filepath.Dir(d.cfg.OutputPath)
	base := filepath.Base(d.cfg.OutputPath)
	return filepath.Join(dir, fmt.Sprintf(".%s.part%d", base, i))
}
