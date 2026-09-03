package elf

import (
	"fmt"

	vmruntime "github.com/vmpacker/internal/runtime"
)

// RuntimeExceptionInvokes converts analyzed unwind plans and final reversed
// bytecode routes into the exact immutable input consumed by runtime.Build.
func (preparation *TranslationPreparation) RuntimeExceptionInvokes() ([]vmruntime.ExceptionInvokeConfig, error) {
	if preparation == nil {
		return nil, fmt.Errorf("translation preparation is required")
	}
	configs := make([]vmruntime.ExceptionInvokeConfig, 0, len(preparation.ExceptionBridges))
	seenFunction := make(map[uint64]struct{}, len(preparation.ExceptionBridges))
	seenThunk := make(map[uint32]struct{})
	for _, bridge := range preparation.ExceptionBridges {
		if bridge.Selection.Address == 0 || bridge.Plan == nil || len(bridge.Plan.Thunks) == 0 {
			return nil, fmt.Errorf("function %q has an incomplete exception bridge plan", bridge.Selection.Name)
		}
		if _, duplicate := seenFunction[bridge.Selection.Address]; duplicate {
			return nil, fmt.Errorf("function 0x%x has duplicate exception bridge plans", bridge.Selection.Address)
		}
		seenFunction[bridge.Selection.Address] = struct{}{}
		if len(bridge.Routes) != len(bridge.Plan.Thunks) {
			return nil, fmt.Errorf("function %q has %d exception thunks but %d final routes", bridge.Selection.Name, len(bridge.Plan.Thunks), len(bridge.Routes))
		}
		planByID := make(map[uint32]struct{}, len(bridge.Plan.Thunks))
		for _, thunk := range bridge.Plan.Thunks {
			if thunk.ID == 0 {
				return nil, fmt.Errorf("function %q has a zero exception thunk identity", bridge.Selection.Name)
			}
			if _, duplicate := seenThunk[thunk.ID]; duplicate {
				return nil, fmt.Errorf("exception thunk 0x%08x is shared by multiple functions", thunk.ID)
			}
			seenThunk[thunk.ID] = struct{}{}
			planByID[thunk.ID] = struct{}{}
		}
		config := vmruntime.ExceptionInvokeConfig{
			FunctionAddress: bridge.Selection.Address,
			Plan:            bridge.Plan,
			Routes:          make([]vmruntime.ExceptionRouteConfig, 0, len(bridge.Routes)),
		}
		for _, route := range bridge.Routes {
			if _, ok := planByID[route.ThunkID]; !ok {
				return nil, fmt.Errorf("function %q final exception route references unknown thunk 0x%08x", bridge.Selection.Name, route.ThunkID)
			}
			if route.FinalVMCallOffset == 0 || route.FinalVMLandingOffset == 0 {
				return nil, fmt.Errorf("function %q exception route 0x%08x has a zero final VM boundary", bridge.Selection.Name, route.ThunkID)
			}
			config.Routes = append(config.Routes, vmruntime.ExceptionRouteConfig{
				ThunkID: route.ThunkID, FinalVMCallOffset: route.FinalVMCallOffset,
				FinalVMLandingOffset: route.FinalVMLandingOffset,
			})
		}
		configs = append(configs, config)
	}
	return configs, nil
}

func (preparation *TranslationPreparation) validateRuntimeExceptionInvokes(image *vmruntime.Image) error {
	configs, err := preparation.RuntimeExceptionInvokes()
	if err != nil {
		return err
	}
	if image == nil {
		return fmt.Errorf("runtime image is required")
	}
	expected := 0
	for _, config := range configs {
		expected += len(config.Routes)
	}
	if len(image.ExceptionInvokes) != expected {
		return fmt.Errorf("runtime image exception invoke count %d does not match prepared count %d", len(image.ExceptionInvokes), expected)
	}
	type key struct {
		function uint64
		thunk    uint32
	}
	actual := make(map[key]vmruntime.ExceptionInvokeImage, len(image.ExceptionInvokes))
	for _, item := range image.ExceptionInvokes {
		itemKey := key{function: item.FunctionAddress, thunk: item.Thunk.ID}
		if _, duplicate := actual[itemKey]; duplicate {
			return fmt.Errorf("runtime image contains duplicate exception invoke function=0x%x thunk=0x%08x", item.FunctionAddress, item.Thunk.ID)
		}
		actual[itemKey] = item
	}
	for _, config := range configs {
		thunkByID := make(map[uint32]int, len(config.Plan.Thunks))
		for index, thunk := range config.Plan.Thunks {
			thunkByID[thunk.ID] = index
		}
		for _, route := range config.Routes {
			item, ok := actual[key{function: config.FunctionAddress, thunk: route.ThunkID}]
			if !ok {
				return fmt.Errorf("runtime image is missing exception invoke function=0x%x thunk=0x%08x", config.FunctionAddress, route.ThunkID)
			}
			thunk := config.Plan.Thunks[thunkByID[route.ThunkID]]
			if item.Personality != config.Plan.Personality ||
				item.PersonalityEncoding != config.Plan.PersonalityEncoding ||
				item.Thunk.OriginalPC != thunk.OriginalPC ||
				item.Thunk.OriginalLandingPad != thunk.OriginalLandingPad ||
				item.FinalVMCallOffset != route.FinalVMCallOffset ||
				item.FinalVMLandingOffset != route.FinalVMLandingOffset {
				return fmt.Errorf("runtime image exception invoke provenance mismatch for function=0x%x thunk=0x%08x", config.FunctionAddress, route.ThunkID)
			}
		}
	}
	return nil
}
