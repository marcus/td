package monitor

import (
	"hash/fnv"
	"strings"
	"time"
)

// modalRenderCache memoizes the rendered issue-detail modal string.
//
// A host application repaints its whole UI on every Bubble Tea message it
// receives — mouse motion, other plugins' ticks, streaming terminal output —
// so an embedded monitor's View() runs orders of magnitude more often than
// the standalone TUI's. Building the modal costs milliseconds and megabytes
// of garbage (styled lines re-wrapped and surface-filled every frame), and
// none of that work changes anything when no input to the rendering changed.
//
// The cache lives on the Model behind a pointer: Model uses value semantics,
// so a plain struct field would be lost between Update and View, while all
// copies share one pointer target.
type modalRenderCache struct {
	key modalRenderKey
	out string
}

// modalRenderKey fingerprints every input the modal renderer reads. It must
// be kept in sync with buildIssueModalView: any field the rendering consumes
// belongs here, or stale output can survive a change to it. Scalar inputs get
// their own fields; everything variable-length is folded into sig by
// modalSignature.
type modalRenderKey struct {
	issueID  string
	width    int
	height   int
	themeRev uint64
	depth    int
	scroll   int
	loading  bool
	embedded bool
	sig      uint64
}

// equal reports whether two keys describe identical rendering inputs.
func (k modalRenderKey) equal(other modalRenderKey) bool { return k == other }

// modalRenderKeyFor builds the fingerprint for the current render inputs.
func (m Model) modalRenderKeyFor(modal *ModalEntry) modalRenderKey {
	return modalRenderKey{
		issueID:  modal.IssueID,
		width:    m.Width,
		height:   m.Height,
		themeRev: m.themeRevision,
		depth:    m.ModalDepth(),
		scroll:   modal.Scroll,
		loading:  modal.Loading,
		embedded: m.Embedded,
		sig:      m.modalSignature(modal),
	}
}

// modalSignature hashes the variable-length inputs of the issue modal:
// error text, footer status, breadcrumb, issue fields, review data,
// dependency/epic lists, rendered markdown sizes, handoff, logs, comments,
// and section focus state.
func (m Model) modalSignature(modal *ModalEntry) uint64 {
	h := fnv.New64a()
	write := func(parts ...string) {
		for _, p := range parts {
			_, _ = h.Write([]byte(p))
			_, _ = h.Write([]byte{0})
		}
	}

	if modal.Error != nil {
		write(modal.Error.Error())
	}
	if !m.Embedded && m.StatusMessage != "" {
		write("status", m.StatusMessage)
	}
	if bc := m.ModalBreadcrumb(); bc != "" {
		write("crumb", bc)
	}

	if issue := modal.Issue; issue != nil {
		write(
			string(issue.ID), string(issue.Type), string(issue.Status), string(issue.Priority),
			issue.Title,
			strings.Join(issue.Labels, ","),
			issue.ImplementerSession, issue.ReviewerSession, issue.ClosedBySession,
			formatTimeSig(issue.CreatedAt),
			formatTimePtrSig(issue.ClosedAt),
			formatTimePtrSig(issue.ReviewedAt),
		)
		write(derefStringSig(issue.DeferUntil), derefStringSig(issue.DueDate))
		writeInt(h, issue.Points)
		writeInt(h, issue.DeferCount)
	}

	if modal.ParentEpic != nil {
		write("parent", string(modal.ParentEpic.ID), modal.ParentEpic.Title)
		if modal.ParentEpicFocused {
			write("parentFocused")
		}
	}

	// The rendered markdown blocks are large (ANSI-styled, kilobytes each),
	// so fold in length plus head/tail slices rather than the full string.
	// Their content only changes via MarkdownRenderedMsg or SetTheme, both of
	// which alter length or themeRevision, which are keyed independently.
	writeStyled := func(tag, s string) {
		write(tag)
		writeInt(h, len(s))
		head := s
		if len(head) > 64 {
			head = head[:64]
		}
		tail := ""
		if len(s) > 64 {
			tail = s[len(s)-64:]
		}
		write(head, tail)
	}
	writeStyled("desc", modal.DescRender)
	writeStyled("accept", modal.AcceptRender)

	if modal.HasActiveApproval {
		write("fresh")
	}
	for _, r := range modal.Reviews {
		superseded := ""
		if r.SupersededAt != nil {
			superseded = "s"
		}
		write(r.ReviewerSession, r.Decision, superseded, formatTimeSig(r.CreatedAt))
	}

	if modal.TaskSectionFocused {
		write("taskFocused")
	}
	writeInt(h, len(modal.EpicTasks))
	writeInt(h, modal.EpicTasksCursor)
	for _, t := range modal.EpicTasks {
		write(string(t.ID), t.Title)
	}

	if modal.BlockedBySectionFocused {
		write("blockedFocused")
	}
	writeInt(h, len(modal.BlockedBy))
	writeInt(h, modal.BlockedByCursor)
	for _, d := range modal.BlockedBy {
		write(string(d.ID), string(d.Status), d.Title)
	}

	if modal.BlocksSectionFocused {
		write("blocksFocused")
	}
	writeInt(h, len(modal.Blocks))
	writeInt(h, modal.BlocksCursor)
	for _, d := range modal.Blocks {
		write(string(d.ID), string(d.Status), d.Title)
	}

	if h := modal.Handoff; h != nil {
		write("handoff", formatTimeSig(h.Timestamp))
		for _, item := range h.Done {
			write(item)
		}
		for _, item := range h.Remaining {
			write(item)
		}
		for _, item := range h.Uncertain {
			write(item)
		}
	}

	writeInt(h, len(modal.Logs))
	for _, l := range modal.Logs {
		write(l.ID, l.Message, string(l.Type), l.SessionID, formatTimeSig(l.Timestamp))
	}

	writeInt(h, len(modal.Comments))
	for _, c := range modal.Comments {
		write(c.SessionID, c.Text, formatTimeSig(c.CreatedAt))
	}

	return h.Sum64()
}

// writeInt mixes an integer into the hash.
func writeInt(h interface{ Write([]byte) (int, error) }, n int) {
	var buf [8]byte
	for i := range buf {
		buf[i] = byte(n >> (i * 8))
	}
	_, _ = h.Write(buf[:])
}

// formatTimeSig renders a time as a stable signature string.
func formatTimeSig(t time.Time) string { return t.Format(time.RFC3339Nano) }

// formatTimePtrSig renders an optional time.
func formatTimePtrSig(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return formatTimeSig(*t)
}

// derefStringSig renders an optional string field.
func derefStringSig(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}
