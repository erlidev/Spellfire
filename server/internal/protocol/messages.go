package protocol

import (
	"errors"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

const (
	ClientJoin    uint64 = 1
	ClientInput   uint64 = 2
	ClientRespawn uint64 = 3
	ClientPing    uint64 = 4
	ClientLoadout uint64 = 5

	ServerWelcome  uint64 = 1
	ServerSnapshot uint64 = 2
	ServerError    uint64 = 3
	ServerPong     uint64 = 4
	// ServerLoadout answers a loadout commit. Unlike ServerError it is not
	// terminal: it carries the authoritative equipped set, and Error when the
	// request was refused, so a rejection never drops the connection.
	ServerLoadout uint64 = 5
	// ServerProgress reports the permanent character axis — level, XP, and the
	// unlock ledger — when it changes. It is pushed, not polled: a level-up has
	// to reach the loadout menu without the player reconnecting to see it.
	ServerProgress uint64 = 6
)

const (
	EntityPlayer     uint64 = 1
	EntityProjectile uint64 = 2
	EntityMob        uint64 = 3
	EntityDrop       uint64 = 4
	EntityNode       uint64 = 5
	EntityTelegraph  uint64 = 6
	EntityDeployable uint64 = 7
	EntityBoss       uint64 = 8
)

const (
	AllegianceSelf    uint64 = 1
	AllegianceSquad   uint64 = 2
	AllegianceNeutral uint64 = 3
	AllegianceHostile uint64 = 4
)

const (
	TelegraphPending  uint64 = 1
	TelegraphActive   uint64 = 2
	TelegraphResolved uint64 = 3
)

type Input struct {
	Sequence uint32
	Buttons  uint32
	AimX     float32
	AimY     float32
	// SelectedSlot is the action-bar slot the use button acts through, bound to
	// 1–6 and carried per input so a mid-frame swap resolves against the slot
	// the player actually had selected.
	SelectedSlot uint32
	ClientTimeMS uint64
}

// Loadout is the equipped set on the wire: content IDs by slot. The repeated
// fields are positional and encode empty slots as empty strings, because a
// slot's index is its binding.
type Loadout struct {
	Weapon  string
	Gadgets []string
	Spells  []string
}

type ClientEnvelope struct {
	Kind         uint64
	SessionToken string
	CharacterID  string
	Input        Input
	ClientTimeMS uint64
	Loadout      Loadout
}

type Entity struct {
	Type              uint64
	ID                string
	Name              string
	ClassName         string
	X, Y              float32
	VX, VY            float32
	AimX, AimY        float32
	Health, MaxHealth float32
	Mana              float32
	AcknowledgedInput uint32
	Alive             bool
	OwnerID           string
	Element           string
	SquadID           string
	Allegiance        uint64
	TelegraphState    uint64
	Invulnerable      bool
	TelegraphShape    string
	Radius            float32
	Length            float32
	Width             float32
	AngleDegrees      float32
	TelegraphProgress float32
	AbilityID         string
	Lingering         bool
	EffectIDs         []string
}

type Collider struct {
	ID     string
	X, Y   float32
	Radius float32
	Kind   string
}

type ServerEnvelope struct {
	Kind               uint64
	ServerTick         uint64
	ServerTimeMS       uint64
	PlayerID           string
	Entities           []Entity
	Colliders          []Collider
	Error              string
	EchoedClientTimeMS uint64
	// Loadout travels on the welcome and on every loadout reply, never on a
	// snapshot: the equipped set changes only in safety, so paying for it 20
	// times a second would be pure waste against the bandwidth budget.
	Loadout *Loadout
	// LoadoutEditable is the authoritative answer to whether the set may be
	// changed from where the body stands, and RespecOwed reports the free
	// respec a balance patch entitled the character to.
	LoadoutEditable bool
	RespecOwed      bool
	// Level, XP, XPToNext, and Unlocks travel on the welcome and on every
	// progress message, never on a snapshot: the permanent axis changes on a
	// kill, not twenty times a second. XPToNext is derived rather than stored,
	// and is sent so the menu reads one curve instead of re-deriving it.
	Level    uint32
	XP       uint64
	XPToNext uint64
	Unlocks  []string
}

func DecodeClient(data []byte) (ClientEnvelope, error) {
	var out ClientEnvelope
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return out, errors.New("invalid protobuf tag")
		}
		data = data[n:]
		switch num {
		case 1:
			v, m := protowire.ConsumeVarint(data)
			if m < 0 {
				return out, errors.New("invalid kind")
			}
			out.Kind = v
			data = data[m:]
		case 2:
			v, m := protowire.ConsumeString(data)
			if m < 0 {
				return out, errors.New("invalid token")
			}
			out.SessionToken = v
			data = data[m:]
		case 3:
			v, m := protowire.ConsumeString(data)
			if m < 0 {
				return out, errors.New("invalid character")
			}
			out.CharacterID = v
			data = data[m:]
		case 4:
			v, m := protowire.ConsumeBytes(data)
			if m < 0 {
				return out, errors.New("invalid input")
			}
			input, err := decodeInput(v)
			if err != nil {
				return out, err
			}
			out.Input = input
			data = data[m:]
		case 5:
			v, m := protowire.ConsumeVarint(data)
			if m < 0 {
				return out, errors.New("invalid time")
			}
			out.ClientTimeMS = v
			data = data[m:]
		case 6:
			v, m := protowire.ConsumeBytes(data)
			if m < 0 {
				return out, errors.New("invalid loadout")
			}
			set, err := decodeLoadout(v)
			if err != nil {
				return out, err
			}
			out.Loadout = set
			data = data[m:]
		default:
			m := protowire.ConsumeFieldValue(num, typ, data)
			if m < 0 {
				return out, errors.New("invalid field")
			}
			data = data[m:]
		}
	}
	return out, nil
}

func decodeInput(data []byte) (Input, error) {
	var out Input
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return out, errors.New("invalid input tag")
		}
		data = data[n:]
		switch num {
		case 1:
			v, m := protowire.ConsumeVarint(data)
			if m < 0 {
				return out, errors.New("invalid sequence")
			}
			out.Sequence = uint32(v)
			data = data[m:]
		case 2:
			v, m := protowire.ConsumeVarint(data)
			if m < 0 {
				return out, errors.New("invalid buttons")
			}
			out.Buttons = uint32(v)
			data = data[m:]
		case 3, 4:
			v, m := protowire.ConsumeFixed32(data)
			if m < 0 {
				return out, errors.New("invalid aim")
			}
			if num == 3 {
				out.AimX = math.Float32frombits(v)
			} else {
				out.AimY = math.Float32frombits(v)
			}
			data = data[m:]
		case 5:
			v, m := protowire.ConsumeVarint(data)
			if m < 0 {
				return out, errors.New("invalid input time")
			}
			out.ClientTimeMS = v
			data = data[m:]
		case 6:
			v, m := protowire.ConsumeVarint(data)
			if m < 0 {
				return out, errors.New("invalid selected slot")
			}
			out.SelectedSlot = uint32(v)
			data = data[m:]
		default:
			m := protowire.ConsumeFieldValue(num, typ, data)
			if m < 0 {
				return out, errors.New("invalid input field")
			}
			data = data[m:]
		}
	}
	return out, nil
}

// decodeLoadout reads the requested set. Order is meaning here: the nth
// repeated entry is slot n, empty string included, so nothing may be skipped.
func decodeLoadout(data []byte) (Loadout, error) {
	var out Loadout
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return out, errors.New("invalid loadout tag")
		}
		data = data[n:]
		switch num {
		case 1, 2, 3:
			v, m := protowire.ConsumeString(data)
			if m < 0 {
				return out, errors.New("invalid loadout slot")
			}
			switch num {
			case 1:
				out.Weapon = v
			case 2:
				out.Gadgets = append(out.Gadgets, v)
			case 3:
				out.Spells = append(out.Spells, v)
			}
			data = data[m:]
		default:
			m := protowire.ConsumeFieldValue(num, typ, data)
			if m < 0 {
				return out, errors.New("invalid loadout field")
			}
			data = data[m:]
		}
	}
	return out, nil
}

func EncodeServer(message ServerEnvelope) []byte {
	var out []byte
	out = appendVarint(out, 1, message.Kind)
	out = appendVarint(out, 2, message.ServerTick)
	out = appendVarint(out, 3, message.ServerTimeMS)
	out = appendString(out, 4, message.PlayerID)
	for _, entity := range message.Entities {
		out = appendMessage(out, 5, encodeEntity(entity))
	}
	for _, collider := range message.Colliders {
		out = appendMessage(out, 6, encodeCollider(collider))
	}
	out = appendString(out, 7, message.Error)
	out = appendVarint(out, 8, message.EchoedClientTimeMS)
	if message.Loadout != nil {
		out = appendMessage(out, 9, encodeLoadout(*message.Loadout))
	}
	if message.LoadoutEditable {
		out = appendVarint(out, 10, 1)
	}
	if message.RespecOwed {
		out = appendVarint(out, 11, 1)
	}
	out = appendVarint(out, 12, uint64(message.Level))
	out = appendVarint(out, 13, message.XP)
	out = appendVarint(out, 14, message.XPToNext)
	for _, unlock := range message.Unlocks {
		out = appendString(out, 15, unlock)
	}
	return out
}

// encodeLoadout writes every slot, empty ones included: appendString drops an
// empty value, and dropping slot 3 would silently promote slot 4 into its
// binding.
func encodeLoadout(set Loadout) []byte {
	out := appendString(nil, 1, set.Weapon)
	for _, id := range set.Gadgets {
		out = appendSlot(out, 2, id)
	}
	for _, id := range set.Spells {
		out = appendSlot(out, 3, id)
	}
	return out
}

func appendSlot(out []byte, field protowire.Number, value string) []byte {
	out = protowire.AppendTag(out, field, protowire.BytesType)
	return protowire.AppendString(out, value)
}

func encodeEntity(e Entity) []byte {
	var out []byte
	out = appendVarint(out, 1, e.Type)
	out = appendString(out, 2, e.ID)
	out = appendString(out, 3, e.Name)
	out = appendString(out, 4, e.ClassName)
	out = appendFloat(out, 5, e.X)
	out = appendFloat(out, 6, e.Y)
	out = appendFloat(out, 7, e.VX)
	out = appendFloat(out, 8, e.VY)
	out = appendFloat(out, 9, e.AimX)
	out = appendFloat(out, 10, e.AimY)
	out = appendFloat(out, 11, e.Health)
	out = appendFloat(out, 12, e.MaxHealth)
	out = appendFloat(out, 13, e.Mana)
	out = appendVarint(out, 14, uint64(e.AcknowledgedInput))
	if e.Alive {
		out = appendVarint(out, 15, 1)
	}
	out = appendString(out, 16, e.OwnerID)
	out = appendString(out, 17, e.Element)
	out = appendString(out, 18, e.SquadID)
	out = appendVarint(out, 19, e.Allegiance)
	out = appendVarint(out, 20, e.TelegraphState)
	if e.Invulnerable {
		out = appendVarint(out, 21, 1)
	}
	out = appendString(out, 22, e.TelegraphShape)
	out = appendFloat(out, 23, e.Radius)
	out = appendFloat(out, 24, e.Length)
	out = appendFloat(out, 25, e.Width)
	out = appendFloat(out, 26, e.AngleDegrees)
	out = appendFloat(out, 27, e.TelegraphProgress)
	out = appendString(out, 28, e.AbilityID)
	if e.Lingering {
		out = appendVarint(out, 29, 1)
	}
	for _, effectID := range e.EffectIDs {
		out = appendString(out, 30, effectID)
	}
	return out
}

func encodeCollider(c Collider) []byte {
	var out []byte
	out = appendString(out, 1, c.ID)
	out = appendFloat(out, 2, c.X)
	out = appendFloat(out, 3, c.Y)
	out = appendFloat(out, 4, c.Radius)
	out = appendString(out, 5, c.Kind)
	return out
}

func appendVarint(out []byte, field protowire.Number, value uint64) []byte {
	if value == 0 {
		return out
	}
	out = protowire.AppendTag(out, field, protowire.VarintType)
	return protowire.AppendVarint(out, value)
}

func appendString(out []byte, field protowire.Number, value string) []byte {
	if value == "" {
		return out
	}
	out = protowire.AppendTag(out, field, protowire.BytesType)
	return protowire.AppendString(out, value)
}

func appendFloat(out []byte, field protowire.Number, value float32) []byte {
	if value == 0 {
		return out
	}
	out = protowire.AppendTag(out, field, protowire.Fixed32Type)
	return protowire.AppendFixed32(out, math.Float32bits(value))
}

func appendMessage(out []byte, field protowire.Number, value []byte) []byte {
	out = protowire.AppendTag(out, field, protowire.BytesType)
	return protowire.AppendBytes(out, value)
}
