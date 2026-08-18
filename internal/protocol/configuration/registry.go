package configuration

// MinimalRegistryData returns the registries needed to describe the Limbo
// overworld. The document is intentionally deterministic so it can later be
// replaced by a pre-serialized template without changing the server flow.
func MinimalRegistryData() *Compound {
	dimension := NewCompound().
		Byte("piglin_safe", 0).
		Byte("natural", 1).
		Float("ambient_light", 0).
		String("infiniburn", "#minecraft:infiniburn_overworld").
		Byte("respawn_anchor_works", 0).
		Byte("has_skylight", 1).
		Byte("bed_works", 1).
		String("effects", "minecraft:overworld").
		Byte("has_raids", 1).
		Int("min_y", -64).
		Int("height", 384).
		Int("logical_height", 384).
		Double("coordinate_scale", 1).
		Byte("ultrawarm", 0).
		Byte("has_ceiling", 0).
		Int("monster_spawn_block_light_limit", 0).
		Int("monster_spawn_light_level", 0)

	dimensionEntry := NewCompound().
		String("name", "minecraft:overworld").
		Int("id", 0).
		Compound("element", dimension)

	biomeEntry := NewCompound().
		String("name", "minecraft:plains").
		Int("id", 0).
		Compound("element", NewCompound().
			Byte("has_precipitation", 1).
			Float("temperature", 0.8).
			Float("downfall", 0.4).
			Compound("effects", NewCompound().
				Int("sky_color", 7907327).
				Int("water_fog_color", 329011).
				Int("fog_color", 12638463).
				Int("water_color", 4159204)))

	chatEntry := NewCompound().
		String("name", "minecraft:chat").
		Int("id", 0).
		Compound("element", NewCompound().
			Compound("chat", NewCompound().
				String("translation_key", "chat.type.text").
				ListOfStrings("parameters", "sender", "content")).
			Compound("narration", NewCompound().
				String("translation_key", "chat.type.text.narrate").
				ListOfStrings("parameters", "sender", "content")))

	return NewCompound().
		Compound("minecraft:dimension_type", NewCompound().
			String("type", "minecraft:dimension_type").
			ListOfCompounds("value", dimensionEntry)).
		Compound("minecraft:worldgen/biome", NewCompound().
			String("type", "minecraft:worldgen/biome").
			ListOfCompounds("value", biomeEntry)).
		Compound("minecraft:chat_type", NewCompound().
			String("type", "minecraft:chat_type").
			ListOfCompounds("value", chatEntry)).
		Compound("minecraft:damage_type", NewCompound().
			String("type", "minecraft:damage_type").
			ListOfCompounds("value", minimalDamageTypes()...))
}

type damageType struct {
	name, messageID, scaling, effects, deathMessageType string
	exhaustion                                          float32
}

// minimalDamageTypes mirrors the complete vanilla 1.20.4 damage type data
// pack. The client resolves these keys while building its Configuration state.
func minimalDamageTypes() []*Compound {
	types := []damageType{
		{"minecraft:arrow", "arrow", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:bad_respawn_point", "badRespawnPoint", "always", "", "intentional_game_design", 0.1},
		{"minecraft:cactus", "cactus", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:cramming", "cramming", "when_caused_by_living_non_player", "", "", 0},
		{"minecraft:dragon_breath", "dragonBreath", "when_caused_by_living_non_player", "", "", 0},
		{"minecraft:drown", "drown", "when_caused_by_living_non_player", "drowning", "", 0},
		{"minecraft:dry_out", "dryout", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:explosion", "explosion", "always", "", "", 0.1},
		{"minecraft:fall", "fall", "when_caused_by_living_non_player", "", "fall_variants", 0},
		{"minecraft:falling_anvil", "anvil", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:falling_block", "fallingBlock", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:falling_stalactite", "fallingStalactite", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:fireball", "fireball", "when_caused_by_living_non_player", "burning", "", 0.1},
		{"minecraft:fireworks", "fireworks", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:fly_into_wall", "flyIntoWall", "when_caused_by_living_non_player", "", "", 0},
		{"minecraft:freeze", "freeze", "when_caused_by_living_non_player", "freezing", "", 0},
		{"minecraft:generic", "generic", "when_caused_by_living_non_player", "", "", 0},
		{"minecraft:generic_kill", "genericKill", "when_caused_by_living_non_player", "", "", 0},
		{"minecraft:hot_floor", "hotFloor", "when_caused_by_living_non_player", "burning", "", 0.1},
		{"minecraft:in_fire", "inFire", "when_caused_by_living_non_player", "burning", "", 0.1},
		{"minecraft:in_wall", "inWall", "when_caused_by_living_non_player", "", "", 0},
		{"minecraft:indirect_magic", "indirectMagic", "when_caused_by_living_non_player", "", "", 0},
		{"minecraft:lava", "lava", "when_caused_by_living_non_player", "burning", "", 0.1},
		{"minecraft:lightning_bolt", "lightningBolt", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:magic", "magic", "when_caused_by_living_non_player", "", "", 0},
		{"minecraft:mob_attack", "mob", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:mob_attack_no_aggro", "mob", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:mob_projectile", "mob", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:on_fire", "onFire", "when_caused_by_living_non_player", "burning", "", 0},
		{"minecraft:out_of_world", "outOfWorld", "when_caused_by_living_non_player", "", "", 0},
		{"minecraft:outside_border", "outsideBorder", "when_caused_by_living_non_player", "", "", 0},
		{"minecraft:player_attack", "player", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:player_explosion", "explosion.player", "always", "", "", 0.1},
		{"minecraft:sonic_boom", "sonic_boom", "always", "", "", 0},
		{"minecraft:stalagmite", "stalagmite", "when_caused_by_living_non_player", "", "", 0},
		{"minecraft:starve", "starve", "when_caused_by_living_non_player", "", "", 0},
		{"minecraft:sting", "sting", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:sweet_berry_bush", "sweetBerryBush", "when_caused_by_living_non_player", "poking", "", 0.1},
		{"minecraft:thorns", "thorns", "when_caused_by_living_non_player", "thorns", "", 0.1},
		{"minecraft:thrown", "thrown", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:trident", "trident", "when_caused_by_living_non_player", "", "", 0.1},
		{"minecraft:unattributed_fireball", "onFire", "when_caused_by_living_non_player", "burning", "", 0.1},
		{"minecraft:wither", "wither", "when_caused_by_living_non_player", "", "", 0},
		{"minecraft:wither_skull", "witherSkull", "when_caused_by_living_non_player", "", "", 0.1},
	}
	entries := make([]*Compound, 0, len(types))
	for id, typ := range types {
		element := NewCompound().String("message_id", typ.messageID).String("scaling", typ.scaling).Float("exhaustion", typ.exhaustion)
		if typ.effects != "" {
			element.String("effects", typ.effects)
		}
		if typ.deathMessageType != "" {
			element.String("death_message_type", typ.deathMessageType)
		}
		entries = append(entries, NewCompound().String("name", typ.name).Int("id", int32(id)).Compound("element", element))
	}
	return entries
}
