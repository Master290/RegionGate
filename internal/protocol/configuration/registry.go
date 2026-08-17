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
			String("precipitation", "rain").
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
			String("chat", "minecraft:chat"))

	return NewCompound().
		Compound("minecraft:dimension_type", NewCompound().
			String("type", "minecraft:dimension_type").
			ListOfCompounds("value", dimensionEntry)).
		Compound("minecraft:worldgen/biome", NewCompound().
			String("type", "minecraft:worldgen/biome").
			ListOfCompounds("value", biomeEntry)).
		Compound("minecraft:chat_type", NewCompound().
			String("type", "minecraft:chat_type").
			ListOfCompounds("value", chatEntry))
}
