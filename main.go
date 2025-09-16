package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"image/color"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
)

// Point represents one CSV row.
type Point struct {
	Year  float64
	Value float64
	Label string
}

// readCSV loads points from a CSV file. Each row is:
// year,value[,label]
func readCSV(path string) ([]Point, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // allow 2 or 3 fields
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("empty CSV")
	}

	var pts []Point
	for i, row := range rows {
		if len(row) < 2 {
			return nil, fmt.Errorf("row %d: expected 2 or 3 columns, got %d", i+1, len(row))
		}
		yearStr := strings.TrimSpace(row[0])
		valStr := strings.TrimSpace(row[1])

		year, err := strconv.ParseFloat(yearStr, 64)
		if err != nil {
			return nil, fmt.Errorf("row %d: invalid year %q: %w", i+1, yearStr, err)
		}

		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			return nil, fmt.Errorf("row %d: invalid value %q: %w", i+1, valStr, err)
		}

		lbl := ""
		if len(row) >= 3 {
			lbl = strings.TrimSpace(row[2])
		}
		if lbl == "" {
			lbl = fmt.Sprintf("%.0f, %.2f", year, val)
		}

		pts = append(pts, Point{Year: year, Value: val, Label: lbl})
	}
	return pts, nil
}

func main() {
	// Define command-line flags
	showYears := flag.Bool("years", false, "show years on x-axis")
	title := flag.String("title", "My Life Line", "title for the timeline")
	flag.Parse()

	// Get positional arguments after flags
	args := flag.Args()
	if len(args) < 2 {
		log.Fatalf("usage: %s [-years] [-title \"Custom Title\"] input.csv output.png\n", filepath.Base(os.Args[0]))
	}

	input := args[0]
	output := args[1]

	points, err := readCSV(input)
	if err != nil {
		log.Fatal(err)
	}

	if len(points) == 0 {
		log.Fatal("no data points")
	}

	// Sort by year so the connecting line goes left->right in time.
	sort.Slice(points, func(i, j int) bool { return points[i].Year < points[j].Year })

	// Calculate density-based scaling for better spacing
	adjustedPoints := make([]Point, len(points))
	copy(adjustedPoints, points)

	fmt.Printf("\n=== Point Adjustment Process ===\n")

	// First pass: handle same-year overlaps with small offsets
	for i := 0; i < len(adjustedPoints); i++ {
		currentYear := adjustedPoints[i].Year
		sameYearCount := 0

		// Count how many events are in the same year (including current)
		for j := 0; j < len(points); j++ {
			if points[j].Year == currentYear {
				sameYearCount++
			}
		}

		// If there are multiple events in the same year, space them out
		if sameYearCount > 1 {
			eventIndex := 0
			// Find which event this is among the same-year events
			for j := 0; j < len(points); j++ {
				if points[j].Year == currentYear {
					if j == i {
						break
					}
					eventIndex++
				}
			}

			// Add small decimal offset: -0.4, -0.2, 0.0, 0.2, 0.4, etc.
			spacing := 0.2
			totalOffset := float64(sameYearCount-1) * spacing / 2
			newYear := currentYear - totalOffset + (float64(eventIndex) * spacing)
			adjustedPoints[i].Year = newYear

			// Log same-year adjustments
			if newYear != currentYear {
				fmt.Printf("Same-year adjustment: '%s' %.0f -> %.1f (event %d of %d in year %.0f)\n",
					adjustedPoints[i].Label, currentYear, newYear, eventIndex+1, sameYearCount, currentYear)
			}
		}
	}

	// Second pass: apply density-based scaling for better distribution
	densityScaledPoints := make([]Point, len(adjustedPoints))
	copy(densityScaledPoints, adjustedPoints)

	// Calculate local density for each point (within a 3-year window)
	densityWindow := 3.0
	densities := make([]float64, len(adjustedPoints))

	for i := 0; i < len(adjustedPoints); i++ {
		count := 0
		for j := 0; j < len(adjustedPoints); j++ {
			if math.Abs(adjustedPoints[j].Year-adjustedPoints[i].Year) <= densityWindow {
				count++
			}
		}
		densities[i] = float64(count)
	}

	// Apply cumulative scaling based on density with normalization
	if len(densityScaledPoints) > 0 {
		minYear := adjustedPoints[0].Year
		maxYear := adjustedPoints[len(adjustedPoints)-1].Year
		totalRange := maxYear - minYear

		// First, calculate all scaled distances
		scaledDistances := make([]float64, len(adjustedPoints))
		totalScaledDistance := 0.0

		for i := 1; i < len(adjustedPoints); i++ {
			// Distance to previous point
			actualDistance := adjustedPoints[i].Year - adjustedPoints[i-1].Year

			// Scale factor based on average density of the two points
			avgDensity := (densities[i] + densities[i-1]) / 2
			scaleFactor := 1.0 + (avgDensity-1.0)*1.5 // Amplify dense areas by up to 150%

			scaledDistances[i] = actualDistance * scaleFactor
			totalScaledDistance += scaledDistances[i]
		}

		// Now normalize and apply positions within the original year range
		densityScaledPoints[0].Year = minYear // Keep first point fixed
		cumulativeScaledDistance := 0.0

		for i := 1; i < len(adjustedPoints); i++ {
			cumulativeScaledDistance += scaledDistances[i]

			// Normalize to fit within original range
			if totalScaledDistance > 0 {
				normalizedPosition := cumulativeScaledDistance / totalScaledDistance
				densityScaledPoints[i].Year = minYear + normalizedPosition*totalRange
			} else {
				densityScaledPoints[i].Year = adjustedPoints[i].Year
			}
		}

		// Print density scaling info
		fmt.Printf("\n=== Density-Based Scaling Results ===\n")

		// Ensure chronological order is maintained (fix any backwards movement)
		for i := 1; i < len(densityScaledPoints); i++ {
			if densityScaledPoints[i].Year <= densityScaledPoints[i-1].Year {
				// If this point would be before or at the same time as the previous, adjust it
				densityScaledPoints[i].Year = densityScaledPoints[i-1].Year + 0.1
			}
		}

		// Show detailed density scaling for all points
		for i := 0; i < len(adjustedPoints); i++ {
			beforeDensityYear := adjustedPoints[i].Year
			afterDensityYear := densityScaledPoints[i].Year

			if math.Abs(afterDensityYear-beforeDensityYear) > 0.1 {
				fmt.Printf("Density scaling: '%s' | Original: %.1f -> After same-year: %.1f -> After density: %.1f | Density: %.0f\n",
					points[i].Label, points[i].Year, beforeDensityYear, afterDensityYear, densities[i])
			} else {
				fmt.Printf("No density change: '%s' | Year: %.1f | Density: %.0f\n",
					points[i].Label, afterDensityYear, densities[i])
			}
		}

		fmt.Printf("=== End Density Scaling ===\n")
	}

	// Use density-scaled points as the final adjusted points
	adjustedPoints = densityScaledPoints

	// Build XY data and labels using adjusted points.
	xy := make(plotter.XYs, len(adjustedPoints))
	lbls := make(plotter.XYs, len(adjustedPoints))
	labels := make([]string, len(adjustedPoints))
	minYear := math.MaxFloat64
	maxYear := -math.MaxFloat64
	minY := 0.0
	maxY := 0.0

	for i, p := range adjustedPoints {
		xy[i].X = p.Year
		xy[i].Y = p.Value
		lbls[i].X = p.Year
		lbls[i].Y = p.Value
		labels[i] = p.Label

		if p.Year < minYear {
			minYear = p.Year
		}
		if p.Year > maxYear {
			maxYear = p.Year
		}
		if p.Value < minY {
			minY = p.Value
		}
		if p.Value > maxY {
			maxY = p.Value
		}
	}

	// Pad ranges a touch.
	yPad := 0.6
	if maxY-minY < 4 { // ensure some vertical breathing room
		yPad = 1.0
	}

	p := plot.New()
	p.Title.Text = *title

	// Configure x-axis based on flag
	if *showYears {
		p.X.Label.Text = "Year"
	} else {
		p.X.Label.Text = ""
		// Hide x-axis tick labels
		p.X.Tick.Label.Font.Size = 0
		p.X.Tick.Length = 0
	}

	p.Y.Label.Text = ""

	// Set x-axis range to start from the smallest year provided
	xMin := math.Floor(minYear)
	xMax := math.Ceil(maxYear)
	p.X.Min = xMin
	p.X.Max = xMax

	// Ensure y shows both positive and negative; if your data is bounded -10..10 you can hardcode:
	if minY > -10 {
		minY = -10
	}
	if maxY < 10 {
		maxY = 10
	}
	p.Y.Min = math.Floor(minY - yPad)
	p.Y.Max = math.Ceil(maxY + yPad)

	// Hide y-axis tick labels and marks
	p.Y.Tick.Label.Font.Size = 0
	p.Y.Tick.Length = 0

	// Optional grid for readability.
	grid := plotter.NewGrid()
	grid.Horizontal.Color = color.Gray{Y: 230}
	grid.Vertical.Color = color.Gray{Y: 245}
	p.Add(grid)

	// Line connecting points.
	line, err := plotter.NewLine(xy)
	if err != nil {
		log.Fatal(err)
	}
	line.Width = vg.Points(1.5)
	line.Color = color.RGBA{A: 255, R: 100, G: 150, B: 200} // Light blue
	p.Add(line)

	// Scatter points.
	sc, err := plotter.NewScatter(xy)
	if err != nil {
		log.Fatal(err)
	}
	sc.Radius = vg.Points(3)
	sc.GlyphStyle.Color = plotutil.Color(1)
	p.Add(sc)

	// Labels (captions) next to each point with alternating positions to avoid overlap.
	for i, point := range adjustedPoints {
		labelData := plotter.XYLabels{
			XYs:    plotter.XYs{{X: point.Year, Y: point.Value}},
			Labels: []string{point.Label},
		}
		l, err := plotter.NewLabels(labelData)
		if err != nil {
			log.Fatal(err)
		}

		// Alternate label positions: above/below and left/right to reduce overlap
		xOffset := vg.Points(8)
		yOffset := vg.Points(8)

		// Alternate between top-right, bottom-right, top-left, bottom-left
		switch i % 4 {
		case 0: // top-right
			l.Offset = vg.Point{X: xOffset, Y: yOffset}
		case 1: // bottom-right
			l.Offset = vg.Point{X: xOffset, Y: -yOffset}
		case 2: // top-left
			l.Offset = vg.Point{X: -xOffset, Y: yOffset}
		case 3: // bottom-left
			l.Offset = vg.Point{X: -xOffset, Y: -yOffset}
		}

		// Make font smaller to reduce label size
		l.TextStyle[0].Font.Size = vg.Points(9)

		p.Add(l)
	}

	// Draw custom x-axis along y=0:
	originY := 0.0

	// x-axis along y=0 across the full x range
	xAxisXY := make(plotter.XYs, 2)
	xAxisXY[0].X, xAxisXY[0].Y = p.X.Min, originY
	xAxisXY[1].X, xAxisXY[1].Y = p.X.Max, originY
	xAxisLine, _ := plotter.NewLine(xAxisXY)
	xAxisLine.Color = color.RGBA{A: 255, R: 200, G: 200, B: 200} // Light grey
	xAxisLine.Width = vg.Points(1.0)
	p.Add(xAxisLine)

	// Configure axis colors based on flag
	if *showYears {
		// Make the x-axis light grey when showing years
		p.X.Color = color.RGBA{A: 255, R: 200, G: 200, B: 200} // Light grey
	} else {
		// Make the x-axis invisible when not showing years
		p.X.Color = color.RGBA{A: 0, R: 0, G: 0, B: 0} // Invisible
	}
	p.Y.Color = color.RGBA{A: 0, R: 0, G: 0, B: 0} // Make y-axis invisible

	// Save output (PNG). Change to .svg if you prefer.
	ext := strings.ToLower(filepath.Ext(output))
	w, h := 12*vg.Inch, 8*vg.Inch // Larger size to accommodate labels
	switch ext {
	case ".png":
		if err := p.Save(w, h, output); err != nil {
			log.Fatal(err)
		}
	case ".svg":
		if err := p.Save(w, h, output); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unsupported output format %q (use .png or .svg)", ext)
	}

	fmt.Printf("Wrote %s\n", output)
}
