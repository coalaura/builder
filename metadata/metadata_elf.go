package metadata

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"math"
)

const (
	elfClass32             = 1
	elfClass64             = 2
	elfSectionReserveIndex = 0xff00
)

type elfLayout struct {
	byteOrder        binary.ByteOrder
	sectionOffset    uint64
	sectionEntrySize uint16
	sectionCount     uint64
	sectionOffsetPos int
	sectionCountPos  int
	sectionSizePos   int
	sectionAlign     uint64
	sectionSize      int
}

func embedELF(data []byte, entries []Entry) ([]byte, error) {
	_, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse ELF metadata target: %w", err)
	}

	layout, err := parseELFLayout(data)
	if err != nil {
		return nil, err
	}

	notes, err := buildELFNotes(layout.byteOrder, entries)
	if err != nil {
		return nil, err
	}

	oldTableSize, ok := multiplyUint64(layout.sectionCount, uint64(layout.sectionEntrySize))
	if !ok || layout.sectionOffset > uint64(len(data)) || oldTableSize > uint64(len(data))-layout.sectionOffset {
		return nil, fmt.Errorf("ELF section table is out of bounds")
	}

	if layout.sectionCount == math.MaxUint64 {
		return nil, fmt.Errorf("ELF section count exceeds format limits")
	}

	noteOffset := alignUint64(uint64(len(data)), 4)
	newTableOffset := alignUint64(noteOffset+uint64(len(notes)), layout.sectionAlign)
	newSectionCount := layout.sectionCount + 1

	newTableSize, ok := multiplyUint64(newSectionCount, uint64(layout.sectionEntrySize))
	if !ok || newTableOffset > uint64(math.MaxInt)-newTableSize {
		return nil, fmt.Errorf("ELF metadata exceeds supported file size")
	}

	if layout.sectionSize == 40 && (noteOffset > math.MaxUint32 || uint64(len(notes)) > math.MaxUint32 || newTableOffset > math.MaxUint32) {
		return nil, fmt.Errorf("ELF32 metadata exceeds 32-bit format limits")
	}

	result := make([]byte, int(newTableOffset+newTableSize))
	copy(result, data)
	copy(result[noteOffset:], notes)

	oldTableEnd := layout.sectionOffset + oldTableSize
	copy(result[newTableOffset:], data[layout.sectionOffset:oldTableEnd])

	newHeader := result[newTableOffset+oldTableSize : newTableOffset+newTableSize]

	writeELFSectionHeader(newHeader, layout, noteOffset, uint64(len(notes)))

	writeELFOffset(result[layout.sectionOffsetPos:], layout, newTableOffset)

	if newSectionCount >= elfSectionReserveIndex {
		layout.byteOrder.PutUint16(result[layout.sectionCountPos:], 0)

		sectionZero := result[newTableOffset : newTableOffset+uint64(layout.sectionEntrySize)]
		writeELFSectionSize(sectionZero[layout.sectionSizePos:], layout, newSectionCount)
	} else {
		layout.byteOrder.PutUint16(result[layout.sectionCountPos:], uint16(newSectionCount))
	}

	return result, nil
}

func parseELFLayout(data []byte) (elfLayout, error) {
	if len(data) < 16 {
		return elfLayout{}, fmt.Errorf("ELF header is truncated")
	}

	var byteOrder binary.ByteOrder

	switch data[elf.EI_DATA] {
	case byte(elf.ELFDATA2LSB):
		byteOrder = binary.LittleEndian
	case byte(elf.ELFDATA2MSB):
		byteOrder = binary.BigEndian
	default:
		return elfLayout{}, fmt.Errorf("ELF byte order is not supported")
	}

	layout := elfLayout{byteOrder: byteOrder}

	switch data[elf.EI_CLASS] {
	case elfClass32:
		if len(data) < 52 {
			return elfLayout{}, fmt.Errorf("ELF32 header is truncated")
		}

		layout.sectionOffset = uint64(byteOrder.Uint32(data[32:]))
		layout.sectionEntrySize = byteOrder.Uint16(data[46:])
		layout.sectionOffsetPos = 32
		layout.sectionCountPos = 48
		layout.sectionSizePos = 20
		layout.sectionAlign = 4
		layout.sectionSize = 40
	case elfClass64:
		if len(data) < 64 {
			return elfLayout{}, fmt.Errorf("ELF64 header is truncated")
		}

		layout.sectionOffset = byteOrder.Uint64(data[40:])
		layout.sectionEntrySize = byteOrder.Uint16(data[58:])
		layout.sectionOffsetPos = 40
		layout.sectionCountPos = 60
		layout.sectionSizePos = 32
		layout.sectionAlign = 8
		layout.sectionSize = 64
	default:
		return elfLayout{}, fmt.Errorf("ELF class is not supported")
	}

	if layout.sectionOffset == 0 || layout.sectionEntrySize < uint16(layout.sectionSize) {
		return elfLayout{}, fmt.Errorf("ELF binary has no valid section table")
	}

	rawCount := byteOrder.Uint16(data[layout.sectionCountPos:])
	if rawCount != 0 {
		layout.sectionCount = uint64(rawCount)

		return layout, nil
	}

	if layout.sectionOffset > uint64(len(data)) || uint64(layout.sectionEntrySize) > uint64(len(data))-layout.sectionOffset {
		return elfLayout{}, fmt.Errorf("ELF section zero is out of bounds")
	}

	sectionZero := data[layout.sectionOffset : layout.sectionOffset+uint64(layout.sectionEntrySize)]
	layout.sectionCount = readELFSectionSize(sectionZero[layout.sectionSizePos:], layout)

	if layout.sectionCount == 0 {
		return elfLayout{}, fmt.Errorf("ELF binary has no sections")
	}

	return layout, nil
}

func buildELFNotes(byteOrder binary.ByteOrder, entries []Entry) ([]byte, error) {
	size := 0

	for _, entry := range entries {
		entrySize := 12 + alignInt(len(entry.Key)+1, 4) + alignInt(len(entry.Value), 4)
		if entrySize < 0 || size > math.MaxInt-entrySize {
			return nil, fmt.Errorf("ELF metadata is too large")
		}

		size += entrySize
	}

	notes := make([]byte, 0, size)
	header := make([]byte, 12)

	for _, entry := range entries {
		byteOrder.PutUint32(header, uint32(len(entry.Key)+1))
		byteOrder.PutUint32(header[4:], uint32(len(entry.Value)))
		byteOrder.PutUint32(header[8:], 0)

		notes = append(notes, header...)
		notes = append(notes, entry.Key...)
		notes = append(notes, 0)
		notes = appendZeroPadding(notes, 4)
		notes = append(notes, entry.Value...)
		notes = appendZeroPadding(notes, 4)
	}

	return notes, nil
}

func writeELFSectionHeader(header []byte, layout elfLayout, offset, size uint64) {
	layout.byteOrder.PutUint32(header[4:], uint32(elf.SHT_NOTE))

	if layout.sectionSize == 40 {
		layout.byteOrder.PutUint32(header[16:], uint32(offset))
		layout.byteOrder.PutUint32(header[20:], uint32(size))
		layout.byteOrder.PutUint32(header[32:], 4)

		return
	}

	layout.byteOrder.PutUint64(header[24:], offset)
	layout.byteOrder.PutUint64(header[32:], size)
	layout.byteOrder.PutUint64(header[48:], 4)
}

func writeELFOffset(destination []byte, layout elfLayout, offset uint64) {
	if layout.sectionSize == 40 {
		layout.byteOrder.PutUint32(destination, uint32(offset))

		return
	}

	layout.byteOrder.PutUint64(destination, offset)
}

func readELFSectionSize(source []byte, layout elfLayout) uint64 {
	if layout.sectionSize == 40 {
		return uint64(layout.byteOrder.Uint32(source))
	}

	return layout.byteOrder.Uint64(source)
}

func writeELFSectionSize(destination []byte, layout elfLayout, size uint64) {
	if layout.sectionSize == 40 {
		layout.byteOrder.PutUint32(destination, uint32(size))

		return
	}

	layout.byteOrder.PutUint64(destination, size)
}

func alignInt(value, alignment int) int {
	return (value + alignment - 1) &^ (alignment - 1)
}

func alignUint64(value, alignment uint64) uint64 {
	return (value + alignment - 1) &^ (alignment - 1)
}

func appendZeroPadding(data []byte, alignment int) []byte {
	padding := alignInt(len(data), alignment) - len(data)

	return append(data, make([]byte, padding)...)
}

func multiplyUint64(left, right uint64) (uint64, bool) {
	if right != 0 && left > math.MaxUint64/right {
		return 0, false
	}

	return left * right, true
}
