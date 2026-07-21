package protocol

import (
	"math"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestDecodeClientInputAndUnknownField(t *testing.T) {
	var input []byte
	input = protowire.AppendTag(input, 1, protowire.VarintType)
	input = protowire.AppendVarint(input, 42)
	input = protowire.AppendTag(input, 2, protowire.VarintType)
	input = protowire.AppendVarint(input, 21)
	input = protowire.AppendTag(input, 3, protowire.Fixed32Type)
	input = protowire.AppendFixed32(input, math.Float32bits(.5))
	input = protowire.AppendTag(input, 4, protowire.Fixed32Type)
	input = protowire.AppendFixed32(input, math.Float32bits(-.25))
	input = protowire.AppendTag(input, 5, protowire.VarintType)
	input = protowire.AppendVarint(input, 123456)
	var envelope []byte
	envelope = protowire.AppendTag(envelope, 1, protowire.VarintType)
	envelope = protowire.AppendVarint(envelope, ClientInput)
	envelope = protowire.AppendTag(envelope, 4, protowire.BytesType)
	envelope = protowire.AppendBytes(envelope, input)
	envelope = protowire.AppendTag(envelope, 99, protowire.VarintType)
	envelope = protowire.AppendVarint(envelope, 7)
	decoded, err := DecodeClient(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != ClientInput || decoded.Input.Sequence != 42 || decoded.Input.Buttons != 21 || decoded.Input.AimX != .5 || decoded.Input.AimY != -.25 || decoded.Input.ClientTimeMS != 123456 {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestDecodeClientRejectsTruncatedMessage(t *testing.T) {
	if _, err := DecodeClient([]byte{0x22, 0x05, 0x08}); err == nil {
		t.Fatal("truncated nested input was accepted")
	}
}

func TestEncodeServerContainsNestedEntitiesAndColliders(t *testing.T) {
	encoded := EncodeServer(ServerEnvelope{Kind: ServerWelcome, ServerTick: 9, PlayerID: "self", Entities: []Entity{{Type: 1, ID: "p1", X: 12.5, Alive: true}}, Colliders: []Collider{{ID: "tree-1", X: 4, Radius: 30, Kind: "tree"}}})
	var entityCount, colliderCount int
	for len(encoded) > 0 {
		number, wire, n := protowire.ConsumeTag(encoded)
		if n < 0 {
			t.Fatal("bad output tag")
		}
		encoded = encoded[n:]
		if number == 5 {
			entityCount++
		}
		if number == 6 {
			colliderCount++
		}
		n = protowire.ConsumeFieldValue(number, wire, encoded)
		if n < 0 {
			t.Fatal("bad output value")
		}
		encoded = encoded[n:]
	}
	if entityCount != 1 || colliderCount != 1 {
		t.Fatalf("nested counts = %d, %d", entityCount, colliderCount)
	}
}
