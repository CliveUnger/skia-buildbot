// Code generated by mockery v1.0.0. DO NOT EDIT.

package mocks

import mock "github.com/stretchr/testify/mock"
import paramtools "go.skia.org/infra/go/paramtools"
import tiling "go.skia.org/infra/go/tiling"
import types "go.skia.org/infra/golden/go/types"

// ComplexTile is an autogenerated mock type for the ComplexTile type
type ComplexTile struct {
	mock.Mock
}

// AllCommits provides a mock function with given fields:
func (_m *ComplexTile) AllCommits() []*tiling.Commit {
	ret := _m.Called()

	var r0 []*tiling.Commit
	if rf, ok := ret.Get(0).(func() []*tiling.Commit); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]*tiling.Commit)
		}
	}

	return r0
}

// DataCommits provides a mock function with given fields:
func (_m *ComplexTile) DataCommits() []*tiling.Commit {
	ret := _m.Called()

	var r0 []*tiling.Commit
	if rf, ok := ret.Get(0).(func() []*tiling.Commit); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]*tiling.Commit)
		}
	}

	return r0
}

// FilledCommits provides a mock function with given fields:
func (_m *ComplexTile) FilledCommits() int {
	ret := _m.Called()

	var r0 int
	if rf, ok := ret.Get(0).(func() int); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(int)
	}

	return r0
}

// FromSame provides a mock function with given fields: completeTile, ignoreRev
func (_m *ComplexTile) FromSame(completeTile *tiling.Tile, ignoreRev int64) bool {
	ret := _m.Called(completeTile, ignoreRev)

	var r0 bool
	if rf, ok := ret.Get(0).(func(*tiling.Tile, int64) bool); ok {
		r0 = rf(completeTile, ignoreRev)
	} else {
		r0 = ret.Get(0).(bool)
	}

	return r0
}

// GetTile provides a mock function with given fields: is
func (_m *ComplexTile) GetTile(is types.IgnoreState) *tiling.Tile {
	ret := _m.Called(is)

	var r0 *tiling.Tile
	if rf, ok := ret.Get(0).(func(types.IgnoreState) *tiling.Tile); ok {
		r0 = rf(is)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*tiling.Tile)
		}
	}

	return r0
}

// IgnoreRules provides a mock function with given fields:
func (_m *ComplexTile) IgnoreRules() paramtools.ParamMatcher {
	ret := _m.Called()

	var r0 paramtools.ParamMatcher
	if rf, ok := ret.Get(0).(func() paramtools.ParamMatcher); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(paramtools.ParamMatcher)
		}
	}

	return r0
}

// SetIgnoreRules provides a mock function with given fields: reducedTile, ignoreRules, irRev
func (_m *ComplexTile) SetIgnoreRules(reducedTile *tiling.Tile, ignoreRules paramtools.ParamMatcher, irRev int64) {
	_m.Called(reducedTile, ignoreRules, irRev)
}

// SetSparse provides a mock function with given fields: sparseCommits, cardinalities
func (_m *ComplexTile) SetSparse(sparseCommits []*tiling.Commit, cardinalities []int) {
	_m.Called(sparseCommits, cardinalities)
}
