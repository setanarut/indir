package indir

// segment represents a byte range of the target file.
type segment struct {
	Index      int   // segment index (0-based)
	Start      int64 // first byte offset in the final file
	End        int64 // last byte offset (inclusive); -1 means unknown (no Content-Length)
	Downloaded int64 // bytes fetched so far for this segment
}

func (s *segment) size() int64 {
	if s.End < 0 {
		return -1
	}
	return s.End - s.Start + 1
}

func (s *segment) isDone() bool {
	return s.End >= 0 && s.Downloaded >= s.size()
}

// splitSegments divides [0, total) into n contiguous, equal-ish byte ranges.
func splitSegments(total int64, n int) []*segment {
	segs := make([]*segment, n)
	each := total / int64(n)
	for i := range segs {
		start := int64(i) * each
		end := start + each - 1
		if i == n-1 {
			end = total - 1 // last segment absorbs any remainder
		}
		segs[i] = &segment{Index: i, Start: start, End: end}
	}
	return segs
}
