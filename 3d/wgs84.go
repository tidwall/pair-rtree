package rtree

import "math"

const degToRad = math.Pi / 180

func TransformLonLatElevToXYZ_WGS84(min, max [3]float64) (minOut, maxOut [3]float64) {
	if min[0] == max[0] && min[1] == max[1] && min[2] == max[2] {
		min = lonLatElevToXYZ_WGS84(min)
		return min, min
	}
	min = lonLatElevToXYZ_WGS84(min)
	max = lonLatElevToXYZ_WGS84(max)
	if min[0] > max[0] {
		min[0], max[0] = max[0], min[0]
	}
	if min[1] > max[1] {
		min[1], max[1] = max[1], min[1]
	}
	if min[2] > max[2] {
		min[2], max[2] = max[2], min[2]
	}
	return min, max
}

func TransformLonLatElevToXYZ_Sphere(min, max [3]float64) (minOut, maxOut [3]float64) {
	if min[0] == max[0] && min[1] == max[1] && min[2] == max[2] {
		min = lonLatElevToXYZ_Sphere(min)
		return min, min
	}
	min = lonLatElevToXYZ_Sphere(min)
	max = lonLatElevToXYZ_Sphere(max)
	if min[0] > max[0] {
		min[0], max[0] = max[0], min[0]
	}
	if min[1] > max[1] {
		min[1], max[1] = max[1], min[1]
	}
	if min[2] > max[2] {
		min[2], max[2] = max[2], min[2]
	}
	return min, max
}

func lonLatElevToXYZ_WGS84(lle [3]float64) (xyz [3]float64) {
	lon, lat, ele := lle[0]*degToRad, lle[1]*degToRad, lle[2]
	// see http://www.mathworks.de/help/toolbox/aeroblks/llatoecefposition.html
	const radius = 6378137.0               // Radius of the Earth (in meters)
	const flattening = 1.0 / 298.257223563 // Flattening factor WGS84 Model
	const ff2 = (1.0 - flattening) * (1.0 - flattening)
	sinLat, cosLat := math.Sin(lat), math.Cos(lat)
	c := 1 / math.Sqrt(cosLat*cosLat+ff2*sinLat*sinLat)
	x := (radius*c + ele) * cosLat * math.Cos(lon)
	y := (radius*c + ele) * cosLat * math.Sin(lon)
	z := (radius*c*ff2 + ele) * sinLat
	return [3]float64{x, z, y} // notice the y and z are switch for rotation
}

func lonLatElevToXYZ_Sphere(lle [3]float64) (xyx [3]float64) {
	const radius = 6378137.0 // Radius of the Earth (in meters)
	lon, lat, ele := lle[0]*degToRad, lle[1]*degToRad, lle[2]
	x := (radius + ele) * math.Cos(lat) * math.Cos(lon)
	y := (radius + ele) * math.Cos(lat) * math.Sin(lon)
	z := (radius + ele) * math.Sin(lat)
	return [3]float64{x, z, y}
}
