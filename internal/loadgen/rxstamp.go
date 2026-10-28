package loadgen

// Stamp is what the receive path learned from the control messages of one
// datagram. Each field is meaningful only when its flag says so.
type Stamp struct {
	KernelNs  int64  // kernel receive time, Unix ns (valid iff Stamped)
	Drops     uint32 // cumulative socket drops (valid iff DropsSeen)
	Stamped   bool   // SCM_TIMESTAMPNS cmsg was present and decoded
	DropsSeen bool   // SO_RXQ_OVFL cmsg was present; otherwise keep the last Drops
	Truncated bool   // MSG_CTRUNC: the control buffer was too small, cmsgs lost
	ParseErr  bool   // the control buffer did not parse
}

// RxStampHealth tallies self-health counters for the receive loop. Observe
// folds one Stamp in; Drops only moves when the drop cmsg was actually seen,
// so an absent cmsg never resets the cumulative counter to zero.
type RxStampHealth struct {
	Reads     uint64
	Unstamped uint64 // reads with no usable kernel timestamp
	Truncated uint64 // reads whose control buffer was truncated
	ParseErrs uint64
	Drops     uint32 // last cumulative drop count seen
}

// Observe records one read.
func (h *RxStampHealth) Observe(s Stamp) {
	h.Reads++
	if !s.Stamped {
		h.Unstamped++
	}
	if s.Truncated {
		h.Truncated++
	}
	if s.ParseErr {
		h.ParseErrs++
	}
	if s.DropsSeen {
		h.Drops = s.Drops
	}
}
