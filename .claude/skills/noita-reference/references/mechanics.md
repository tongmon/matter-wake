# Mechanics map

Where the analysis covers each mechanic. `C49` is a section of `clone-spec/COMPLETION_PROGRESS.txt` (`noitaref section C49`); a NAME in capitals is a document (`noitaref doc NAME`); a lowercase name is an executable model in the analysis's `tools/` (`noitaref model NAME`). Read the related rows too.

| Mechanic | Rules | Models | Material fields (`-fields`) | Related |
|---|---|---|---|---|
| Cell tick: order, work regions, double updates, request queues | MATERIAL_TICK, C33, C40, C112, C113 | native_probe_cell_update, native_probe_queue_flush, native_probe_queue_merge, native_probe_worker_merge_order | | parallel groups, sleep |
| Parallel groups and barriers | PRIORITY_RESULTS (part 1), C121, C122 | model_worker_halo, native_probe_worker_bounds | | cell tick |
| Sleep and wake | MATERIAL_TICK (parts 2 and 4), PRIORITY_RESULTS (part 2), C09, C25, C33, C43, C47, C56, C112, C113, RUNTIME_RESULTS (R05); raw logs `analysis-detail/runtime-priority/sleep-natural-*.json` | native_probe_sleep_wake, model_worker_residency, native_probe_cell_update, native_probe_sand_grid, native_probe_liquid_grid | | cell tick, every movement row, liquid collisions (reinsertion wakes), liquid particles (return wakes) |
| Powder (sand) movement, density exchange, sticking | C47, C51, REPORT_PHASE2 (density exchange probability) | native_probe_sand_grid, native_probe_density_exchange, native_probe_sand_attachment, native_probe_attached_update | `density,liquid_gravity,liquid_sand,liquid_viscosity,liquid_sticks_to_ceiling,liquid_static` | liquids, liquid collisions, sleep |
| Viscous materials | C48 | native_probe_slime_grid | `density,liquid_gravity,liquid_slime,liquid_viscosity` | powder, liquids |
| Liquid fall and sideways flow | C49, REPORT_MATERIALS (part 3) | native_probe_liquid_grid | `density,liquid_gravity,liquid_flow_speed,liquid_damping,liquid_viscosity,liquid_static` | liquid collisions, liquid particles, sleep, powder, viscous (C48 shows the same diagonal-push rule) |
| Liquid collisions and relocation upward | C50, C114, C115 | native_probe_cell_reinsert, native_probe_reinsert_scheduler, model_reinsert_chain, native_probe_liquid_transfer | | liquids, liquid particles, sleep |
| Liquid particles (splash) | C81, C82, C83, C84, C85 | model_particle_integration, model_particle_sweep, model_particle_retire, model_particle_bounce, native_probe_particle_transfer | | liquids, liquid collisions (particles return through its queue), explosions |
| Gas | C24, C56 | native_probe_gas_motion, native_probe_gas_update, native_probe_gas_swap | `gas_speed,gas_upwards_speed,gas_horizontal_speed,gas_downwards_speed,lifetime` | fire, reactions |
| Fire and burning | C55, C57, C61, C62 | native_probe_fire_update, native_probe_burning_update, native_probe_burning_propagation, native_probe_ignition, native_probe_gas_ignition | `burnable,fire_hp,autoignition_temperature,temperature_of_fire,generates_flames,generates_smoke,requires_oxygen,on_fire` | gas, reactions, temperature |
| Reactions and conversion | C52, C54, REPORT_MATERIALS | native_probe_reaction_select, native_probe_reaction_apply, native_probe_required_reaction, native_probe_material_conversion | | fire, temperature |
| Temperature, freezing, melting | C53 | native_probe_temperature_select | `cold_freezes,warmth_melts,autoignition_temperature` | reactions |
| Cell HP, explosions, projectiles | C66, C67, C73, C74, C75 | native_probe_explosion_ray, model_explosion_spatial, native_probe_explosion_hole, native_probe_solid_explosion | `hp,durability,crackability` | destruction, liquid particles |
| Rigid bodies in liquids (buoyancy, drag) | C31, C41, C42, C26 | native_probe_buoyancy, native_probe_contact_drag | `solid_,density` | destruction, liquids |
| Destruction: pieces, contours, convex shapes | DESTRUCTION_BRIDGE, C34, C35, C68, C69, C70, C71, C72, C76, C77, C78, C79, C80, C92 to C108 | native_probe_pixel_components, native_probe_pixel_contour, model_convex_merge, model_triangulation, model_tess_topology | `solid_,durability` | rigid bodies, explosions |
| Streaming, residency, saving | WORLD_STREAMING, C43, C122 | model_worker_residency | | sleep |
| Frame order and RNG | FRAME_ORDER, C23, C118 | model_distribution_rng | | cell tick |
| Electricity (optional) | C123 to C127 | model_electricity_move, model_electricity_io, model_electric_charge, model_electricity_init | `electrical_conductivity` | fire |
| Player collision (reference only) | C44, C45, C63, PLAYER_MOVEMENT | native_probe_player_collision_path | | |

## Finding the Noita material

`noitaref material NAME` takes Noita's internal names. Closest matches for matter-wake's materials:

| matter-wake | Noita | Note |
|---|---|---|
| sand | `sand` | Noita stores powders as liquids: `cell_type` 1 with `liquid_sand` true. |
| water | `water` | |
| stone | `rock_static` | Static terrain is `cell_type` 1 with `liquid_static` true. Noita's `cell_type` 3 (solid) means rigid-body materials such as wood or steel. |

`cell_type` values (C38): 1 liquid, 2 gas, 3 solid, 4 fire. For a new material, search its English name (`noitaref search oil`) or a tag such as `[liquid]`.

## Keeping this map current

Add a row, a section or a model whenever a search finds a source this map lacked. A missing row is how the water values were missed.
