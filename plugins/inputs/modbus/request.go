package modbus

import (
	"math"
	"sort"

	"github.com/influxdata/telegraf"
)

type request struct {
	address uint16
	length  uint16
	fields  []field
}

func countRegisters(requests []request) uint64 {
	var l uint64
	for _, r := range requests {
		l += uint64(r.length)
	}
	return l
}

// Only split too-large groups, but ignore all optimization potential
func splitMaxBatchSize(g request, maxBatchSize uint16) []request {
	var requests []request

	idx := 0
	for start := g.address; start < g.address+g.length; {
		current := request{
			address: start,
		}

		// Initialize the end to a safe value avoiding infinite loops
		end := g.address + g.length
		var batchEnd uint16
		if start >= math.MaxUint16-maxBatchSize {
			batchEnd = math.MaxUint16
		} else {
			batchEnd = start + maxBatchSize
		}
		for _, f := range g.fields[idx:] {
			// If the current field exceeds the batch size we need to split
			// the request here
			if f.address+f.length > batchEnd {
				break
			}
			// End of field still fits into the batch so add it to the request
			current.fields = append(current.fields, f)
			end = f.address + f.length
			idx++
		}

		if end > g.address+g.length {
			end = g.address + g.length
		}
		if idx >= len(g.fields) || g.fields[idx].address >= end {
			current.length = end - start
		} else {
			current.length = g.fields[idx].address - start
		}
		start = end

		if len(current.fields) > 0 {
			requests = append(requests, current)
		}
	}

	return requests
}

func shrinkGroup(g request, maxBatchSize uint16) []request {
	var requests []request
	var current request

	for _, f := range g.fields {
		// Just add the field and update length if we are still
		// within the maximum batch-size
		if current.length > 0 && f.address+f.length <= current.address+maxBatchSize {
			current.fields = append(current.fields, f)
			current.length = f.address - current.address + f.length
			continue
		}

		// Ignore completely empty requests
		if len(current.fields) > 0 {
			requests = append(requests, current)
		}

		// Create a new request
		current = request{
			fields:  []field{f},
			address: f.address,
			length:  f.length,
		}
	}
	if len(current.fields) > 0 {
		requests = append(requests, current)
	}

	return requests
}

func optimizeGroup(g request, maxBatchSize uint16) []request {
	if len(g.fields) == 0 {
		return nil
	}

	requests := shrinkGroup(g, maxBatchSize)
	length := countRegisters(requests)

	for i := 1; i < len(g.fields)-1; i++ {
		// Always keep consecutive fields as they are known to be optimal
		if g.fields[i-1].address+g.fields[i-1].length == g.fields[i].address {
			continue
		}

		// Perform the split and check if it is better
		// Note: This involves recursive optimization of the right side of the split.
		current := shrinkGroup(request{fields: g.fields[:i]}, maxBatchSize)
		current = append(current, optimizeGroup(request{fields: g.fields[i:]}, maxBatchSize)...)
		currentLength := countRegisters(current)

		// Do not allow for more requests
		if len(current) > len(requests) {
			continue
		}
		// Try to reduce the number of registers we are trying to access
		if currentLength >= length {
			continue
		}

		// We found a better solution
		requests = current
		length = currentLength
	}

	return requests
}

func optimizeGroupWithinLimits(g request, params groupingParams) []request {
	if len(g.fields) == 0 {
		return nil
	}

	var requests []request
	currentRequest := request{
		fields:  []field{g.fields[0]},
		address: g.fields[0].address,
		length:  g.fields[0].length,
	}
	for i := 1; i <= len(g.fields)-1; i++ {
		// Check if we need to interrupt the current chunk and require a new one
		holeSize := g.fields[i].address - (g.fields[i-1].address + g.fields[i-1].length)
		if g.fields[i].address < g.fields[i-1].address+g.fields[i-1].length {
			params.log.Warnf(
				"Request at %d with length %d overlaps with next request at %d",
				g.fields[i-1].address, g.fields[i-1].length, g.fields[i].address,
			)
			holeSize = 0
		}
		needInterrupt := holeSize > params.maxExtraRegisters                                                     // too far apart
		needInterrupt = needInterrupt || currentRequest.length+holeSize+g.fields[i].length > params.maxBatchSize // too large
		if !needInterrupt {
			// Still safe to add the field to the current request
			currentRequest.length = g.fields[i].address + g.fields[i].length - currentRequest.address
			currentRequest.fields = append(currentRequest.fields, g.fields[i])
			continue
		}
		// Finish the current request, add it to the list and construct a new one
		requests = append(requests, currentRequest)
		currentRequest = request{
			fields:  []field{g.fields[i]},
			address: g.fields[i].address,
			length:  g.fields[i].length,
		}
	}
	requests = append(requests, currentRequest)
	return requests
}

type groupingParams struct {
	// Maximum size of a request in registers
	maxBatchSize uint16
	// optimization to use for grouping register groups to requests, Also put potential optimization parameters here
	optimization      string
	maxExtraRegisters uint16
	// Will force reads to start at zero (if possible) while respecting the max-batch size.
	enforceFromZero bool
	// tags to add for the requests
	tags map[string]string
	// log facility to inform the user
	log telegraf.Logger
}

func groupFieldsToRequests(fields []field, params groupingParams) []request {
	if len(fields) == 0 {
		return nil
	}

	// Sort the fields by address (ascending) and length
	sort.Slice(fields, func(i, j int) bool {
		addrI := fields[i].address
		addrJ := fields[j].address
		return addrI < addrJ || (addrI == addrJ && fields[i].length > fields[j].length)
	})

	// Construct the consecutive register chunks for the addresses and construct Modbus requests.
	// For field addresses like [1, 2, 3, 5, 6, 10, 11, 12, 14] we should construct the following
	// requests (1, 3) , (5, 2) , (10, 3), (14 , 1). Furthermore, we should respect field boundaries
	// and the given maximum chunk sizes.
	var groups []request
	var current request
	for _, f := range fields {
		// Add tags from higher up
		if f.tags == nil {
			f.tags = make(map[string]string, len(params.tags))
		}
		for k, v := range params.tags {
			f.tags[k] = v
		}

		// Check if we need to interrupt the current chunk and require a new one
		if current.length > 0 && f.address == current.address+current.length {
			// Still safe to add the field to the current request
			current.length += f.length
			if !f.omit {
				current.fields = append(current.fields, f)
			}
			continue
		}

		// Finish the current request, add it to the list and construct a new one
		if current.length > 0 && len(fields) > 0 {
			groups = append(groups, current)
		}
		current = request{
			address: f.address,
			length:  f.length,
		}
		if !f.omit {
			current.fields = append(current.fields, f)
		}
	}
	if current.length > 0 && len(fields) > 0 {
		groups = append(groups, current)
	}

	if len(groups) == 0 {
		return nil
	}

	// Enforce the first read to start at zero if the option is set
	if params.enforceFromZero {
		groups[0].length += groups[0].address
		groups[0].address = 0
	}

	var requests []request
	switch params.optimization {
	case "shrink":
		// Shrink request by striping leading and trailing fields with an omit flag set
		for _, g := range groups {
			if len(g.fields) > 0 {
				requests = append(requests, shrinkGroup(g, params.maxBatchSize)...)
			}
		}
	case "rearrange":
		// Allow rearranging fields between request in order to reduce the number of touched
		// registers while keeping the number of requests
		for _, g := range groups {
			if len(g.fields) > 0 {
				requests = append(requests, optimizeGroup(g, params.maxBatchSize)...)
			}
		}
	case "aggressive":
		// Allow rearranging fields similar to "rearrange" but allow mixing of groups
		// This might reduce the number of requests at the cost of more registers being touched.
		var total request
		for _, g := range groups {
			if len(g.fields) > 0 {
				total.fields = append(total.fields, g.fields...)
			}
		}
		requests = optimizeGroup(total, params.maxBatchSize)
	case "max_insert":
		// Similar to aggressive but keeps the number of touched registers below a threshold
		var total request
		for _, g := range groups {
			if len(g.fields) > 0 {
				total.fields = append(total.fields, g.fields...)
			}
		}
		requests = optimizeGroupWithinLimits(total, params)
	default:
		// no optimization
		for _, g := range groups {
			if len(g.fields) > 0 {
				requests = append(requests, splitMaxBatchSize(g, params.maxBatchSize)...)
			}
		}
	}

	return requests
}
