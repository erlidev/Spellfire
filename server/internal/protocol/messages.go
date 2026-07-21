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

	ServerWelcome  uint64 = 1
	ServerSnapshot uint64 = 2
	ServerError    uint64 = 3
	ServerPong     uint64 = 4
)

type Input struct {
	Sequence     uint32
	Buttons      uint32
	AimX         float32
	AimY         float32
	ClientTimeMS uint64
}

type ClientEnvelope struct {
	Kind         uint64
	SessionToken string
	CharacterID  string
	Input        Input
	ClientTimeMS uint64
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
	return out
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
