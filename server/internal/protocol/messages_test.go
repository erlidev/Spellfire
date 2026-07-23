package protocol

import (
	"bytes"
	"fmt"
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

func TestDecodeClientPreservesInteractButton(t *testing.T) {
	var input []byte
	input = protowire.AppendTag(input, 1, protowire.VarintType)
	input = protowire.AppendVarint(input, 1)
	input = protowire.AppendTag(input, 2, protowire.VarintType)
	input = protowire.AppendVarint(input, 128)
	var envelope []byte
	envelope = protowire.AppendTag(envelope, 1, protowire.VarintType)
	envelope = protowire.AppendVarint(envelope, ClientInput)
	envelope = protowire.AppendTag(envelope, 4, protowire.BytesType)
	envelope = protowire.AppendBytes(envelope, input)
	decoded, err := DecodeClient(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Input.Buttons != 128 {
		t.Fatalf("buttons = %d, want INTERACT bit 128", decoded.Input.Buttons)
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

func TestEncodeServerCarriesExpandedEntityState(t *testing.T) {
	entity := Entity{
		Type: EntityTelegraph, ID: "warning", OwnerID: "caster", Element: "fire", SquadID: "squad-a",
		Allegiance: AllegianceHostile, TelegraphState: TelegraphResolved, Invulnerable: true,
		TelegraphShape: "cone", Radius: 20, Length: 200, Width: 30, AngleDegrees: 60,
		TelegraphProgress: .75, AbilityID: "fire-cone", Lingering: true, EffectIDs: []string{"burn", "slow"}, Mass: -1, Deleting: true, DeleteProgress: .5,
	}
	encoded := EncodeServer(ServerEnvelope{Kind: ServerSnapshot, Entities: []Entity{entity}})
	nested := firstMessageField(t, encoded, 5)
	fields := map[protowire.Number]int{}
	for len(nested) > 0 {
		number, wire, n := protowire.ConsumeTag(nested)
		if n < 0 {
			t.Fatal("bad entity tag")
		}
		nested = nested[n:]
		fields[number]++
		n = protowire.ConsumeFieldValue(number, wire, nested)
		if n < 0 {
			t.Fatalf("bad entity field %d", number)
		}
		nested = nested[n:]
	}
	for field := protowire.Number(17); field <= 33; field++ {
		if fields[field] == 0 {
			t.Fatalf("expanded field %d missing from wire", field)
		}
	}
	if fields[30] != 2 {
		t.Fatalf("effect_ids occurrences = %d, want 2", fields[30])
	}
}

func TestEncodeServerCarriesBoxCollisionComponent(t *testing.T) {
	collider := Collider{ID: "wall:0", EntityID: "wall", Kind: "wall", Shape: "box", X: 10, Y: 20, Width: 96, Height: 96}
	nested := firstMessageField(t, EncodeServer(ServerEnvelope{Kind: ServerSnapshot, Colliders: []Collider{collider}}), 6)
	fields := map[protowire.Number]bool{}
	for len(nested) > 0 {
		number, wire, n := protowire.ConsumeTag(nested)
		if n < 0 {
			t.Fatal("bad collision tag")
		}
		nested = nested[n:]
		fields[number] = true
		n = protowire.ConsumeFieldValue(number, wire, nested)
		if n < 0 {
			t.Fatalf("bad collision field %d", number)
		}
		nested = nested[n:]
	}
	for _, field := range []protowire.Number{1, 2, 3, 5, 6, 7, 8, 9} {
		if !fields[field] {
			t.Fatalf("collision field %d missing", field)
		}
	}
}

func firstMessageField(t *testing.T, encoded []byte, wanted protowire.Number) []byte {
	t.Helper()
	for len(encoded) > 0 {
		number, wire, n := protowire.ConsumeTag(encoded)
		if n < 0 {
			t.Fatal("bad envelope tag")
		}
		encoded = encoded[n:]
		if number == wanted {
			value, m := protowire.ConsumeBytes(encoded)
			if m < 0 {
				t.Fatal("bad nested message")
			}
			return value
		}
		n = protowire.ConsumeFieldValue(number, wire, encoded)
		if n < 0 {
			t.Fatal("bad envelope field")
		}
		encoded = encoded[n:]
	}
	t.Fatalf("message field %d not found", wanted)
	return nil
}

func TestSnapshotBandwidthBudget(t *testing.T) {
	const (
		snapshotBudget = 64 * 1024
		sendRate       = 20
	)
	baseline := bandwidthFixture(20, 40, 0, 30, false)
	expanded := bandwidthFixture(20, 40, 0, 30, true)
	typical := bandwidthFixture(20, 40, 10, 30, true)
	dense := bandwidthFixture(100, 200, 25, 80, true)
	baselineBytes, expandedBytes := len(EncodeServer(baseline)), len(EncodeServer(expanded))
	typicalBytes, denseBytes := len(EncodeServer(typical)), len(EncodeServer(dense))
	t.Logf("representative before=%d after=%d delta=%d; with telegraphs=%d; dense=%d (%d bytes/s at %d Hz)",
		baselineBytes, expandedBytes, expandedBytes-baselineBytes, typicalBytes, denseBytes, denseBytes*sendRate, sendRate)
	if denseBytes > snapshotBudget {
		t.Fatalf("dense snapshot = %d bytes, exceeds %d-byte budget", denseBytes, snapshotBudget)
	}
}

func bandwidthFixture(players, projectiles, telegraphs, colliders int, expanded bool) ServerEnvelope {
	message := ServerEnvelope{Kind: ServerSnapshot, ServerTick: 123456, ServerTimeMS: 1784664000000, PlayerID: "player-000-aaaaaaaa"}
	for index := 0; index < players; index++ {
		entity := Entity{
			Type: EntityPlayer, ID: fmt.Sprintf("player-%03d-aaaaaaaa", index), Name: fmt.Sprintf("Adventurer%03d", index), ClassName: "gunslinger",
			X: float32(index*17 - 400), Y: float32(index*11 - 200), VX: 123.5, VY: -42.25, AimX: .75, AimY: -.25,
			Health: 87, MaxHealth: 100, Mana: 7, AcknowledgedInput: uint32(1000 + index), Alive: true,
		}
		if expanded {
			entity.Element, entity.SquadID, entity.Allegiance = "fire", fmt.Sprintf("squad-%02d", index/4), AllegianceHostile
			entity.EffectIDs = []string{"burn-minor", "slow-minor"}
			entity.Invulnerable, entity.Lingering = index == 0, index == players-1
		}
		message.Entities = append(message.Entities, entity)
	}
	for index := 0; index < projectiles; index++ {
		entity := Entity{
			Type: EntityProjectile, ID: fmt.Sprintf("projectile-%04d", index), ClassName: "fireball",
			X: float32(index*9 - 500), Y: float32(index*7 - 300), VX: 592.8, VY: -120, OwnerID: fmt.Sprintf("player-%03d-aaaaaaaa", index%max(players, 1)), Alive: true,
		}
		if expanded {
			entity.Element, entity.Allegiance = "fire", AllegianceHostile
		}
		message.Entities = append(message.Entities, entity)
	}
	for index := 0; index < telegraphs; index++ {
		message.Entities = append(message.Entities, Entity{
			Type: EntityTelegraph, ID: fmt.Sprintf("telegraph-%04d", index), OwnerID: fmt.Sprintf("player-%03d-aaaaaaaa", index%max(players, 1)),
			X: float32(index * 19), Y: float32(index * -13), AimX: .8, AimY: .2, Element: "fire", Allegiance: AllegianceHostile,
			TelegraphState: TelegraphPending, TelegraphShape: "line", Length: 889.2, Width: 58, TelegraphProgress: .6,
			AbilityID: "fire-bolt-cast", Alive: true,
		})
	}
	for index := 0; index < colliders; index++ {
		id := fmt.Sprintf("tree-%03d", index)
		message.Entities = append(message.Entities, Entity{Type: EntityWorldItem, ID: id, ClassName: "tree", X: float32(index * 31), Y: float32(index * -23), Health: 500, MaxHealth: 500, Alive: true, Mass: -1, Radius: 37, Allegiance: AllegianceNeutral})
		message.Colliders = append(message.Colliders, Collider{ID: id + ":0", EntityID: id, X: float32(index * 31), Y: float32(index * -23), Radius: 37, Kind: "tree", Shape: "circle"})
	}
	return message
}

// The selected action-bar slot rides every input, so the server resolves the
// use button against the slot the player actually had selected.
func TestDecodeClientInputCarriesTheSelectedSlot(t *testing.T) {
	var input []byte
	input = protowire.AppendTag(input, 1, protowire.VarintType)
	input = protowire.AppendVarint(input, 9)
	input = protowire.AppendTag(input, 6, protowire.VarintType)
	input = protowire.AppendVarint(input, 5)
	var envelope []byte
	envelope = protowire.AppendTag(envelope, 1, protowire.VarintType)
	envelope = protowire.AppendVarint(envelope, ClientInput)
	envelope = protowire.AppendTag(envelope, 4, protowire.BytesType)
	envelope = protowire.AppendBytes(envelope, input)
	decoded, err := DecodeClient(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Input.SelectedSlot != 5 {
		t.Fatalf("selected slot = %d, want 5", decoded.Input.SelectedSlot)
	}
}

// Slot order is meaning: an empty slot must survive the wire as an empty slot,
// or slot four would silently inherit slot three's binding.
func TestLoadoutWirePreservesEmptySlots(t *testing.T) {
	var set []byte
	set = protowire.AppendTag(set, 1, protowire.BytesType)
	set = protowire.AppendString(set, "starter-staff")
	for _, id := range []string{"", "", "fire-bolt"} {
		set = protowire.AppendTag(set, 3, protowire.BytesType)
		set = protowire.AppendString(set, id)
	}
	var envelope []byte
	envelope = protowire.AppendTag(envelope, 1, protowire.VarintType)
	envelope = protowire.AppendVarint(envelope, ClientLoadout)
	envelope = protowire.AppendTag(envelope, 6, protowire.BytesType)
	envelope = protowire.AppendBytes(envelope, set)
	decoded, err := DecodeClient(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != ClientLoadout || decoded.Loadout.Weapon != "starter-staff" {
		t.Fatalf("decoded = %#v", decoded)
	}
	if len(decoded.Loadout.Spells) != 3 || decoded.Loadout.Spells[2] != "fire-bolt" || decoded.Loadout.Spells[0] != "" {
		t.Fatalf("spell slots = %#v", decoded.Loadout.Spells)
	}
}

// The reply must round-trip the same way, and a snapshot must not carry a
// loadout at all — it changes only in safety, so paying for it every frame
// would be waste against the bandwidth budget.
func TestEncodeServerCarriesLoadoutOnlyWhenSet(t *testing.T) {
	reply := EncodeServer(ServerEnvelope{
		Kind: ServerLoadout, PlayerID: "p1", LoadoutEditable: true,
		Loadout: &Loadout{Weapon: "starter-staff", Spells: []string{"", "fire-bolt"}},
	})
	snapshot := EncodeServer(ServerEnvelope{Kind: ServerSnapshot, PlayerID: "p1"})
	if !bytes.Contains(reply, []byte("fire-bolt")) {
		t.Fatalf("reply lost the equipped set: %x", reply)
	}
	for _, field := range []protowire.Number{9, 10, 11} {
		if hasField(snapshot, field) {
			t.Fatalf("snapshot carries loadout field %d", field)
		}
	}
	if !hasField(reply, 9) || !hasField(reply, 10) {
		t.Fatalf("reply is missing its loadout fields: %x", reply)
	}
}

func hasField(data []byte, want protowire.Number) bool {
	for len(data) > 0 {
		number, wire, n := protowire.ConsumeTag(data)
		if n < 0 {
			return false
		}
		if number == want {
			return true
		}
		data = data[n:]
		m := protowire.ConsumeFieldValue(number, wire, data)
		if m < 0 {
			return false
		}
		data = data[m:]
	}
	return false
}

// A craft request is positional in nothing — slots are named — and an omitted
// slot stays omitted rather than becoming an empty reference. Recipe validation
// is responsible for rejecting required blanks.
func TestCraftRequestRoundTrips(t *testing.T) {
	envelope := appendMessage(appendVarint(nil, 1, ClientCraft), 7,
		appendMessage(appendString(nil, 1, "starter-rifle"), 2,
			appendString(appendString(nil, 1, "receiver"), 2, "rifle-receiver")))
	decoded, err := DecodeClient(envelope)
	if err != nil {
		t.Fatalf("decode craft: %v", err)
	}
	if decoded.Kind != ClientCraft || decoded.Craft.Weapon != "starter-rifle" {
		t.Fatalf("decoded = %+v", decoded)
	}
	if decoded.Craft.Components["receiver"] != "rifle-receiver" || len(decoded.Craft.Components) != 1 {
		t.Fatalf("components = %v", decoded.Craft.Components)
	}
	// An empty component is not a part and must not survive as a reference.
	empty := appendMessage(appendVarint(nil, 1, ClientCraft), 7,
		appendMessage(appendString(nil, 1, "starter-rifle"), 2, appendString(nil, 1, "barrel")))
	decoded, err = DecodeClient(empty)
	if err != nil {
		t.Fatalf("decode incomplete craft: %v", err)
	}
	if len(decoded.Craft.Components) != 0 {
		t.Fatalf("an empty slot survived as %v", decoded.Craft.Components)
	}
}

// Owned items and carried materials ride the welcome and craft replies. Empty
// stacks are dropped, and slots go out sorted so one item has one encoding.
func TestStacksDropEmptiesAndSort(t *testing.T) {
	stacks := Stacks(map[string]int{"tempered-plate": 2, "salvaged-plate": 4, "resonant-plate": 0})
	if len(stacks) != 2 || stacks[0].Material != "salvaged-plate" || stacks[1].Count != 2 {
		t.Fatalf("stacks = %+v", stacks)
	}
	item := CraftedItem{ID: "itm-1", Weapon: "starter-rifle", Components: map[string]string{"receiver": "rifle-receiver", "barrel": "service-rifle-barrel"}}
	if string(encodeItem(item)) != string(encodeItem(item)) {
		t.Fatal("one item produced two encodings")
	}
}
