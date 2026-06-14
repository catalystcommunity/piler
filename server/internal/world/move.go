package world

import "github.com/catalystcommunity/piler/server/internal/csil"

// applyMove applies a sub-tile delta (dx, dy) to a position and clamps the
// result to the field bounds [0, fieldW] x [0, fieldH] (in sub-units). All
// math is done in absolute sub-units, then split back into tile + sub-tile
// offset. layer is unchanged.
func applyMove(pos csil.Position, dx, dy int64, sub, fieldW, fieldH uint64) csil.Position {
	s := int64(sub)
	totalX := clampI64(pos.TileX*s+int64(pos.SubX)+dx, 0, int64(fieldW))
	totalY := clampI64(pos.TileY*s+int64(pos.SubY)+dy, 0, int64(fieldH))
	return posFromTotal(totalX, totalY, s, pos.Layer)
}

// posFromTotal splits absolute sub-unit coordinates back into tile + sub.
// Inputs are expected non-negative (callers clamp to >= 0), so the sub
// offset lands in [0, sub).
func posFromTotal(totalX, totalY, sub, layer int64) csil.Position {
	tx := floorDiv(totalX, sub)
	ty := floorDiv(totalY, sub)
	return csil.Position{
		TileX: tx,
		TileY: ty,
		SubX:  uint64(totalX - tx*sub),
		SubY:  uint64(totalY - ty*sub),
		Layer: layer,
	}
}

// floorDiv is integer division rounding toward negative infinity (Go's /
// truncates toward zero, which is wrong for negative coordinates).
func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func clampI64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func absI64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// clampStep limits a delta's magnitude to max (used to cap bot speed/tick).
func clampStep(d, max int64) int64 {
	if d > max {
		return max
	}
	if d < -max {
		return -max
	}
	return d
}
