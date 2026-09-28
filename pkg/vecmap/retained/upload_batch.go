package retained

import "slices"

// Next returns one bounded batch or nil when no work remains. Only one batch can
// be outstanding. Uploads take priority while acknowledged residency plus the
// batch payload fits the residency limits; they follow target mesh then texture
// order. Stale versions retire in admission order only when capacity requires it
// or when no upload remains, so a new target never waits behind the previous
// cover's retirement. It never skips an oversized resource or silently exceeds a
// budget: ErrBudget requires a larger explicit byte budget.
func (p *Planner) Next(budget Budget) (*Batch, error) {
	if p.resident == nil {
		return nil, ErrInput
	}
	if p.pending != nil {
		return nil, ErrBusy
	}
	releases := budget.releases()
	if budget.Bytes == 0 || budget.Bytes > p.limits.Bytes || budget.Resources <= 0 || budget.Resources > p.limits.Resources || releases <= 0 || releases > p.limits.Resources {
		return nil, ErrBudget
	}
	batch, err := p.uploadBatch(budget)
	if err != nil || len(batch.Uploads) == 0 || !p.fitsResident(batch) {
		// Retirement reclaims capacity for an upload that cannot coexist with
		// stale residency, and still precedes an unplannable target's error.
		if retire := p.retireBatch(releases); len(retire.Releases) > 0 {
			batch, err = retire, nil
		}
	}
	if err != nil {
		return nil, err
	}
	if len(batch.Releases) == 0 && len(batch.Uploads) == 0 {
		return nil, nil
	}
	if p.nextTicket == 0 {
		return nil, ErrLimit
	}
	batch.Ticket = p.nextTicket
	p.nextTicket++
	// Keep private descriptor slices so caller edits cannot forge an acknowledgement.
	p.pending = &Batch{Ticket: batch.Ticket, Uploads: slices.Clone(batch.Uploads), Releases: slices.Clone(batch.Releases), Bytes: batch.Bytes}
	return batch, nil
}

// fitsResident reports whether acknowledged residency, including stale versions
// awaiting retirement, can admit the batch without exceeding the limits. Once no
// stale version remains, residency is a subset of active plus target, which
// SetTarget already proved to fit, so an upload batch always fits eventually.
func (p *Planner) fitsResident(batch *Batch) bool {
	return p.residentBytes+batch.Bytes <= p.limits.Bytes && len(p.resident)+len(batch.Uploads) <= p.limits.Resources
}

func (p *Planner) retireBatch(maximum int) *Batch {
	batch := &Batch{}
	for _, key := range p.residentOrder {
		_, active := p.activeSet[key]
		_, desired := p.targetSet[key]
		if active || desired {
			continue
		}
		batch.Releases = append(batch.Releases, key)
		if len(batch.Releases) == maximum {
			break
		}
	}
	return batch
}

func (p *Planner) uploadBatch(budget Budget) (*Batch, error) {
	// Preflight all missing resources so an impossible job cannot appear to be
	// making progress indefinitely while hiding an oversized indivisible upload.
	for _, r := range p.targetOrder {
		if _, ready := p.resident[r.Version]; !ready && r.bytes() > budget.Bytes {
			return nil, ErrBudget
		}
	}
	batch := &Batch{}
	for _, r := range p.targetOrder {
		if _, ready := p.resident[r.Version]; ready {
			continue
		}
		if len(batch.Uploads) == budget.Resources || r.bytes() > budget.Bytes-batch.Bytes {
			break
		}
		batch.Uploads = append(batch.Uploads, r)
		batch.Bytes += r.bytes()
	}
	return batch, nil
}
