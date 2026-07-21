package game

import (
	"sort"
	"time"
)

const combatEventCapacity = 1024

// CombatEventKind identifies the stable events downstream systems consume.
// Damage events drive live feedback and boss totals; kill events freeze the
// whole per-life ledger so a later drop does not have to inspect mutable state.
type CombatEventKind uint8

const (
	CombatDamage CombatEventKind = iota + 1
	CombatKill
)

// DamageContribution is one source's effective health damage against a target.
// Shield absorption and overkill are deliberately excluded: contribution is
// the health a source actually removed.
type DamageContribution struct {
	SourceID string
	Amount   float64
}

// CombatEvent is the public, append-only combat-log surface. Sequence is a
// cursor: consumers retain the last sequence they processed and ask for newer
// events without reaching into World internals.
type CombatEvent struct {
	Sequence      uint64
	At            time.Time
	Kind          CombatEventKind
	SourceID      string
	TargetID      string
	Amount        float64
	CreditID      string
	Contributions []DamageContribution
}

type contribution struct {
	amount        float64
	firstSequence uint64
}

// combatLog owns per-target, per-life ledgers and a bounded event stream. The
// event stream is intentionally bounded for the long-running shared world;
// durable consumers must advance their cursor rather than treating it as a
// database.
type combatLog struct {
	next          uint64
	capacity      int
	events        []CombatEvent
	contributions map[string]map[string]contribution
	lastKills     map[string]CombatEvent
}

func newCombatLog(capacity int) *combatLog {
	if capacity < 1 {
		capacity = combatEventCapacity
	}
	return &combatLog{
		capacity: capacity, contributions: make(map[string]map[string]contribution),
		lastKills: make(map[string]CombatEvent),
	}
}

func (l *combatLog) recordDamage(at time.Time, sourceID, targetID string, amount float64, lethal bool) {
	if amount <= 0 {
		return
	}
	sequence := l.nextSequence()
	if sourceID != "" {
		ledger := l.contributions[targetID]
		if ledger == nil {
			ledger = make(map[string]contribution)
			l.contributions[targetID] = ledger
		}
		entry := ledger[sourceID]
		if entry.firstSequence == 0 {
			entry.firstSequence = sequence
		}
		entry.amount += amount
		ledger[sourceID] = entry
	}
	l.append(CombatEvent{
		Sequence: sequence, At: at, Kind: CombatDamage,
		SourceID: sourceID, TargetID: targetID, Amount: amount,
	})
	if !lethal {
		return
	}
	contributions := l.targetContributions(targetID)
	kill := CombatEvent{
		Sequence: l.nextSequence(), At: at, Kind: CombatKill,
		SourceID: sourceID, TargetID: targetID,
		CreditID: creditFor(contributions), Contributions: contributions,
	}
	l.append(kill)
	l.lastKills[targetID] = cloneCombatEvent(kill)
}

func (l *combatLog) nextSequence() uint64 {
	l.next++
	return l.next
}

func (l *combatLog) append(event CombatEvent) {
	l.events = append(l.events, event)
	if overflow := len(l.events) - l.capacity; overflow > 0 {
		copy(l.events, l.events[overflow:])
		l.events = l.events[:l.capacity]
	}
}

func (l *combatLog) targetContributions(targetID string) []DamageContribution {
	ledger := l.contributions[targetID]
	result := make([]DamageContribution, 0, len(ledger))
	for sourceID, entry := range ledger {
		result = append(result, DamageContribution{SourceID: sourceID, Amount: entry.amount})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Amount != result[j].Amount {
			return result[i].Amount > result[j].Amount
		}
		return ledger[result[i].SourceID].firstSequence < ledger[result[j].SourceID].firstSequence
	})
	return result
}

func creditFor(contributions []DamageContribution) string {
	if len(contributions) == 0 {
		return ""
	}
	return contributions[0].SourceID
}

func (l *combatLog) eventsAfter(sequence uint64) []CombatEvent {
	result := make([]CombatEvent, 0)
	for _, event := range l.events {
		if event.Sequence > sequence {
			result = append(result, cloneCombatEvent(event))
		}
	}
	return result
}

func (l *combatLog) lastKill(targetID string) (CombatEvent, bool) {
	event, ok := l.lastKills[targetID]
	return cloneCombatEvent(event), ok
}

// resetTarget starts a new per-life ledger while leaving immutable events in
// the stream for drop/ranking consumers that have not advanced yet.
func (l *combatLog) resetTarget(targetID string) {
	delete(l.contributions, targetID)
	delete(l.lastKills, targetID)
}

func cloneCombatEvent(event CombatEvent) CombatEvent {
	event.Contributions = append([]DamageContribution(nil), event.Contributions...)
	return event
}
