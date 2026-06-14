# Positioning model

> Design intent / working proposal. Captures the maintainer's notes; the
> exact numeric representation is not yet locked.

## World shape

The world is a set of discrete **rooms** (FF6-style), not one continuous
walked scene. You move *between* rooms (doors, edges, warps) rather than
scrolling a single infinite map in cardinal directions. Each room is a
grid of **tiles**.

## Layered objects at sub-tile precision

Objects are **layered** and may sit at any tile **and sub-tile**
coordinate. Examples the maintainer called out:

- a character standing **mid-tile**,
- a desk placed **3/10 of a tile** into a tile.

So position is not "which tile" alone — it is a continuous-ish point
within the room, plus a stacking order.

## Proposed representation

A position within a room:

```
struct Pos {
    tile_x: i32,     // integer tile column
    tile_y: i32,     // integer tile row
    sub_x:  u16,     // fixed-point offset within the tile, [0, SUB)
    sub_y:  u16,     // fixed-point offset within the tile, [0, SUB)
    layer:  i16,     // stacking / draw + collision layer (z)
}
```

- **`SUB`** is the sub-tile resolution (e.g. `SUB = 256` or `1000`). "3/10
  of a tile" = `sub_x = 0.3 * SUB`. Fixed-point (integer) avoids float
  drift and makes positions exactly reproducible across client and server
  — important since the server is authoritative and clients are untrusted.
  Final `SUB` TBD; pick a value that divides cleanly into the fractions we
  care about and leaves headroom.
- **`layer`** orders objects that share a tile: floor < rug < desk <
  item-on-desk < character < overhead. Layer also informs what blocks
  movement vs. what is purely decorative.

### Rendering vs. logic

- **Logic** (collision, interaction range, occupancy) uses the
  fixed-point position directly.
- **Rendering** maps `tile + sub/SUB` to pixels: `px = tile * TILE_PX +
  sub * TILE_PX / SUB`. Draw order = `layer`, then `tile_y`, then `sub_y`
  for within-layer depth sorting.

## Open questions

- Per-object **footprint** (a desk occupies more than a point) — likely a
  sub-tile-sized AABB or tile-mask, separate from its anchor `Pos`.
- **Movement**: continuous interpolation between server snapshots vs.
  tile-stepped. Leaning continuous given sub-tile precision.
- **Room boundaries / transitions**: how a `Pos` near an edge maps to the
  adjacent room and the hand-off of the moving entity.
- Whether `layer` is a small fixed enum or a free integer.
