// windows-icon converts a brand PNG into a multi-size Windows ICO.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"

	"golang.org/x/image/draw"
)

func main() {
	sourcePath := flag.String("source", "internal/brand/logo.png", "source PNG")
	iconSizes := flag.String("sizes", "16,32,48,128,256", "comma-separated square icon sizes")
	out := flag.String("out", "", "output ICO")
	flag.Parse()
	if *out == "" {
		panic("-out is required")
	}
	file, err := os.Open(*sourcePath)
	if err != nil {
		panic(err)
	}
	source, err := png.Decode(file)
	file.Close()
	if err != nil {
		panic(err)
	}
	var sizes []int
	for _, raw := range strings.Split(*iconSizes, ",") {
		size, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || size < 1 || size > 256 {
			panic("sizes must be comma-separated integers from 1 to 256")
		}
		sizes = append(sizes, size)
	}
	chunks := make([][]byte, len(sizes))
	offset := 6 + 16*len(sizes)
	var output bytes.Buffer
	binary.Write(&output, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	for i, size := range sizes {
		scaled := image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.CatmullRom.Scale(scaled, scaled.Bounds(), source, source.Bounds(), draw.Over, nil)
		var data bytes.Buffer
		if err := png.Encode(&data, scaled); err != nil {
			panic(err)
		}
		chunks[i] = data.Bytes()
		dimension := byte(size)
		output.Write([]byte{dimension, dimension, 0, 0})
		binary.Write(&output, binary.LittleEndian, [2]uint16{1, 32})
		binary.Write(&output, binary.LittleEndian, [2]uint32{uint32(data.Len()), uint32(offset)})
		offset += data.Len()
	}
	for _, chunk := range chunks {
		output.Write(chunk)
	}
	if err := os.WriteFile(*out, output.Bytes(), 0644); err != nil {
		panic(err)
	}
}
