package tracking

import (
	"fmt"
	"strconv"
)

func itoa(n int) string { return strconv.Itoa(n) }

func scanYM(name string, y, m *int) (int, error) {
	return fmt.Sscanf(name, "location_points_%04d_%02d", y, m)
}
