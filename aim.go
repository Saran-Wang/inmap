package main

import (
	"bitbucket.org/ctessum/aim/lib.aim"
	"bitbucket.org/ctessum/sparse"
	"bufio"
	"code.google.com/p/lvd.go/cdf"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
)

const (
	xFactor = 1 // x, y, and z factors to increase grid resolution by
	yFactor = 1
	zFactor = 1
)

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
var scenario = flag.String("scenario", "", "name of scenario to run")
var vehicle = flag.String("vehicle", "", "vehicle name")

func main() {
	flag.Parse()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			panic(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	if *scenario == "" {
		fmt.Println("Need to specify scenario")
		os.Exit(1)
	}
	if *vehicle == "" {
		fmt.Println("Need to specify vehicle")
		os.Exit(1)
	}

	runtime.GOMAXPROCS(8)

	//const basedir = "/home/marshall/tessumcm/src/bitbucket.org/ctessum/aim/"
	const basedir = "/home/chris/go/src/bitbucket.org/ctessum/aim/"
	fmt.Println("Reading input data...")
	d := aim.InitAIMdata(basedir + "wrf2aim/aimData.ncf")
	fmt.Println("Reading plume rise information...")
	p := aim.GetPlumeRiseInfo(basedir + "wrf2aim/aimData.ncf")
	//	createImage(m.Ubins.Subset([]int{0, 0, 0, 0},
	//	[]int{0, 0, m.Ubins.Shape[2] - 1, m.Ubins.Shape[3] - 1}), "Ubins")

	const (
		height   = 75. * 0.3048             // m
		diam     = 11.28 * 0.3048           // m
		temp     = (377.-32)*5./9. + 273.15 // K
		velocity = 61.94 * 1097. / 3600.    // m/hr
	)

	//var emisDir = "/home/marshall/tessumcm/GREET_spatial/output/FuelOptions_aim/" + *scenario + "/na12/"
	var emisDir = basedir

	//	emissions := getEmissions("gasoline_na12.csv",m)
	emissions := getEmissionsNCF(emisDir+
		*scenario+"."+*vehicle+".groundlevel.ncf", d)
	elevatedEmis := getEmissionsNCF(emisDir+
		*scenario+"."+*vehicle+".elevated.ncf", d)

	// apply plume rise
	for pol, elev := range elevatedEmis {
		for j := 0; j < elev.Shape[1]; j++ {
			for i := 0; i < elev.Shape[2]; i++ {
				k := p.CalcPlumeRise(height, diam, temp, velocity, j, i)
				emissions[pol].AddVal(elev.Get(0, j, i), k, j, i)
			}
		}
	}
	// create images
	//	for pol, Cf := range emissions {
	//		createImage(Cf.Subset([]int{0, 0, 0},
	//		[]int{0, Cf.Shape[1] - 1, Cf.Shape[2] - 1}), pol)
	//	}
	//emissions["NH3"] = emissions["PM2_5"].Copy()

	finalConc := d.Run(emissions)

	// write data out to netcdf
	h := cdf.NewHeader(
		[]string{"nx", "ny", "nz"},
		[]int{d.Nx, d.Ny, d.Nz})
	for pol, _ := range finalConc {
		h.AddVariable(pol, []string{"nz", "ny", "nx"}, []float32{0})
		h.AddAttribute(pol, "units", "ug m-3")
	}
	h.Define()
	ff, err := os.Create(basedir + *scenario + ".ncf")
	if err != nil {
		panic(err)
	}
	f, err := cdf.Create(ff, h) // writes the header to ff
	if err != nil {
		panic(err)
	}
	for pol, arr := range finalConc {
		writeNCF(f, pol, arr)
	}
	ff.Close()
}

func getEmissionsNCF(filename string, d *aim.AIMdata) (
	emissions map[string]*sparse.DenseArray) {

	const massConv = 907184740000.       // μg per short ton
	const timeConv = 3600. * 8760.       // seconds per year
	const emisConv = massConv / timeConv // convert tons/year to μg/s

	emissions = make(map[string]*sparse.DenseArray)
	ff, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	f, err := cdf.Open(ff)
	if err != nil {
		panic(err)
	}
	defer ff.Close()
	for _, Var := range f.Header.Variables() {
		if Var == "CO" || Var == "PM10" || Var == "CH4" {
			continue
		}
		emissions[polTrans(Var)] = sparse.ZerosDense(d.Nz, d.Ny, d.Nx)
		dims := f.Header.Lengths(Var)
		nread := 1
		for _, dim := range dims {
			nread *= dim
		}
		r := f.Reader(Var, nil, nil)
		buf := r.Zero(nread)
		_, err = r.Read(buf)
		if err != nil {
			panic(err)
		}
		dat := buf.([]float32)
		for i, val := range dat {
			emissions[polTrans(Var)].Elements[i] = float64(val) * emisConv
		}
	}
	return
}

func polTrans(pol string) string {
	switch pol {
	case "PM2.5":
		return "PM2_5"
	default:
		return pol
	}
}

func getEmissions(filename string, d *aim.AIMdata) (
	emissions map[string]*sparse.DenseArray) {

	const massConv = 907184740000.       // μg per short ton
	const timeConv = 3600. * 8760.       // seconds per year
	const emisConv = massConv / timeConv // convert tons/year to μg/s

	emissions = make(map[string]*sparse.DenseArray)
	emissions["VOC"] = sparse.ZerosDense(d.Nz, d.Ny, d.Nx)
	emissions["PM2_5"] = sparse.ZerosDense(d.Nz, d.Ny, d.Nx)
	emissions["NH3"] = sparse.ZerosDense(d.Nz, d.Ny, d.Nx)
	emissions["SOx"] = sparse.ZerosDense(d.Nz, d.Ny, d.Nx)
	emissions["NOx"] = sparse.ZerosDense(d.Nz, d.Ny, d.Nx)

	f, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	scanner := bufio.NewScanner(f)
	firstLine := true
	polCols := make(map[string]int)
	for scanner.Scan() {
		line := strings.Split(scanner.Text(), ",")
		if firstLine {
			for i, pol := range line {
				polCols[pol] = i
			}
			firstLine = false
			continue
		}
		row, col := s2i(line[polCols["row"]])-1, s2i(line[polCols["col"]])-1
		SOx := s2f(line[polCols["SOx"]])
		VOC := s2f(line[polCols["VOC"]])
		PM2_5 := s2f(line[polCols["PM2.5"]])
		NH3 := s2f(line[polCols["NH3"]])
		NOx := s2f(line[polCols["NOx"]])

		emissions["SOx"].Set(SOx*emisConv, 0, row, col)
		emissions["VOC"].Set(VOC*emisConv, 0, row, col)
		emissions["PM2_5"].Set(PM2_5*emisConv, 0, row, col)
		emissions["NH3"].Set(NH3*emisConv, 0, row, col)
		emissions["NOx"].Set(NOx*emisConv, 0, row, col)
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	return
}

func s2i(s string) int {
	i, err := strconv.ParseInt(s, 0, 64)
	if err != nil {
		panic(err)
	}
	return int(i)
}
func s2f(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		panic(err)
	}
	return f
}

func writeNCF(f *cdf.File, Var string, data *sparse.DenseArray) {
	data32 := make([]float32, len(data.Elements))
	for i, e := range data.Elements {
		data32[i] = float32(e)
	}
	end := f.Header.Lengths(Var)
	start := make([]int, len(end))
	w := f.Writer(Var, start, end)
	_, err := w.Write(data32)
	if err != nil {
		panic(err)
	}
}
