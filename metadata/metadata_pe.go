package metadata

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	pe32Magic                   = 0x10b
	pe64Magic                   = 0x20b
	peResourceDirectoryIndex    = 2
	peCertificateDirectoryIndex = 4
	peResourceTypeRCDATA        = 10
	peResourceDirectoryFlag     = uint32(1 << 31)
	peSectionInitializedData    = 0x00000040
	peSectionMemoryRead         = 0x40000000
	peResourceLimit             = 1 << 16
)

type peLayout struct {
	peOffset                 int
	optionalOffset           int
	optionalSize             int
	sectionTableOffset       int
	sectionCount             int
	sectionAlignment         uint32
	fileAlignment            uint32
	sizeOfHeaders            uint32
	sizeOfInitializedDataPos int
	sizeOfImagePos           int
	resourceDirectoryPos     int
	certificateDirectoryPos  int
}

type peSection struct {
	virtualSize    uint32
	virtualAddress uint32
	rawSize        uint32
	rawOffset      uint32
}

type peResourceIdentifier struct {
	name []uint16
	id   uint32
}

type peResourceNode struct {
	characteristics uint32
	timestamp       uint32
	majorVersion    uint16
	minorVersion    uint16
	children        []*peResourceChild
	offset          uint32
}

type peResourceChild struct {
	identifier peResourceIdentifier
	node       *peResourceNode
	leaf       *peResourceLeaf
	nameOffset uint32
}

type peResourceLeaf struct {
	data            []byte
	codePage        uint32
	reserved        uint32
	dataEntryOffset uint32
	dataOffset      uint32
}

type peResourceParser struct {
	data       []byte
	sections   []peSection
	rootOffset int
	rootSize   int
	active     map[uint32]struct{}
	count      int
}

func embedPE(data []byte, entries []Entry) ([]byte, error) {
	layout, sections, err := parsePELayout(data)
	if err != nil {
		return nil, err
	}

	certificateSize := binary.LittleEndian.Uint32(data[layout.certificateDirectoryPos+4:])
	if certificateSize != 0 {
		return nil, fmt.Errorf("cannot embed metadata in a signed PE binary")
	}

	resourceRVA := binary.LittleEndian.Uint32(data[layout.resourceDirectoryPos:])
	resourceSize := binary.LittleEndian.Uint32(data[layout.resourceDirectoryPos+4:])

	root := &peResourceNode{}

	if resourceRVA != 0 || resourceSize != 0 {
		if resourceRVA == 0 || resourceSize == 0 {
			return nil, fmt.Errorf("PE resource directory is malformed")
		}

		rootOffset, ok := peRVAToOffset(resourceRVA, resourceSize, sections, len(data))
		if !ok {
			return nil, fmt.Errorf("PE resource directory is out of bounds")
		}

		parser := peResourceParser{
			data:       data,
			sections:   sections,
			rootOffset: rootOffset,
			rootSize:   int(resourceSize),
			active:     make(map[uint32]struct{}),
		}

		root, err = parser.parseNode(0)
		if err != nil {
			return nil, err
		}
	}

	err = addPEMetadata(root, entries)
	if err != nil {
		return nil, err
	}

	sectionTableEnd := layout.sectionTableOffset + layout.sectionCount*40
	minimumRawOffset := uint32(len(data))

	for _, section := range sections {
		if section.rawSize != 0 && section.rawOffset < minimumRawOffset {
			minimumRawOffset = section.rawOffset
		}
	}

	if sectionTableEnd+40 > int(layout.sizeOfHeaders) || sectionTableEnd+40 > int(minimumRawOffset) {
		return nil, fmt.Errorf("PE headers have no space for a metadata resource section")
	}

	virtualEnd := uint64(0)

	for _, section := range sections {
		size := max(section.virtualSize, section.rawSize)

		end := uint64(section.virtualAddress) + uint64(size)
		if end > virtualEnd {
			virtualEnd = end
		}
	}

	newVirtualAddress := alignUint64(virtualEnd, uint64(layout.sectionAlignment))
	newRawOffset := alignUint64(uint64(len(data)), uint64(layout.fileAlignment))

	if newVirtualAddress > math.MaxUint32 || newRawOffset > math.MaxUint32 {
		return nil, fmt.Errorf("PE metadata section offset exceeds 32-bit format limits")
	}

	resourceData, err := serializePEResources(root, uint32(newVirtualAddress))
	if err != nil {
		return nil, err
	}

	newRawSize := alignUint64(uint64(len(resourceData)), uint64(layout.fileAlignment))
	newImageSize := alignUint64(newVirtualAddress+uint64(len(resourceData)), uint64(layout.sectionAlignment))

	if newRawSize > math.MaxUint32 || newImageSize > math.MaxUint32 || newRawOffset+newRawSize > uint64(math.MaxInt) {
		return nil, fmt.Errorf("PE metadata section exceeds format limits")
	}

	oldInitializedSize := binary.LittleEndian.Uint32(data[layout.sizeOfInitializedDataPos:])
	if uint64(oldInitializedSize)+newRawSize > math.MaxUint32 {
		return nil, fmt.Errorf("PE initialized data size exceeds format limits")
	}

	result := make([]byte, int(newRawOffset+newRawSize))
	copy(result, data)
	copy(result[newRawOffset:], resourceData)

	binary.LittleEndian.PutUint16(result[layout.peOffset+6:], uint16(layout.sectionCount+1))
	binary.LittleEndian.PutUint32(result[layout.sizeOfInitializedDataPos:], oldInitializedSize+uint32(newRawSize))
	binary.LittleEndian.PutUint32(result[layout.sizeOfImagePos:], uint32(newImageSize))
	binary.LittleEndian.PutUint32(result[layout.resourceDirectoryPos:], uint32(newVirtualAddress))
	binary.LittleEndian.PutUint32(result[layout.resourceDirectoryPos+4:], uint32(len(resourceData)))

	sectionHeader := result[sectionTableEnd : sectionTableEnd+40]
	copy(sectionHeader, ".rsrc")

	binary.LittleEndian.PutUint32(sectionHeader[8:], uint32(len(resourceData)))
	binary.LittleEndian.PutUint32(sectionHeader[12:], uint32(newVirtualAddress))
	binary.LittleEndian.PutUint32(sectionHeader[16:], uint32(newRawSize))
	binary.LittleEndian.PutUint32(sectionHeader[20:], uint32(newRawOffset))
	binary.LittleEndian.PutUint32(sectionHeader[36:], peSectionInitializedData|peSectionMemoryRead)

	return result, nil
}

func parsePELayout(data []byte) (peLayout, []peSection, error) {
	if len(data) < 0x40 {
		return peLayout{}, nil, fmt.Errorf("PE DOS header is truncated")
	}

	if uint64(len(data)) > math.MaxUint32 {
		return peLayout{}, nil, fmt.Errorf("PE binary exceeds 32-bit file offset limits")
	}

	peOffset := int(binary.LittleEndian.Uint32(data[0x3c:]))
	if peOffset < 0x40 || peOffset > len(data)-24 || string(data[peOffset:peOffset+4]) != "PE\x00\x00" {
		return peLayout{}, nil, fmt.Errorf("PE header is malformed")
	}

	sectionCount := int(binary.LittleEndian.Uint16(data[peOffset+6:]))

	optionalSize := int(binary.LittleEndian.Uint16(data[peOffset+20:]))
	optionalOffset := peOffset + 24

	if optionalSize < 2 || optionalOffset > len(data)-optionalSize {
		return peLayout{}, nil, fmt.Errorf("PE optional header is truncated")
	}

	layout := peLayout{
		peOffset:                 peOffset,
		optionalOffset:           optionalOffset,
		optionalSize:             optionalSize,
		sectionTableOffset:       optionalOffset + optionalSize,
		sectionCount:             sectionCount,
		sizeOfInitializedDataPos: optionalOffset + 8,
		sizeOfImagePos:           optionalOffset + 56,
	}

	var (
		directoryOffset      int
		directoryCountOffset int
	)

	switch binary.LittleEndian.Uint16(data[optionalOffset:]) {
	case pe32Magic:
		directoryCountOffset = optionalOffset + 92
		directoryOffset = optionalOffset + 96
	case pe64Magic:
		directoryCountOffset = optionalOffset + 108
		directoryOffset = optionalOffset + 112
	default:
		return peLayout{}, nil, fmt.Errorf("PE optional header format is not supported")
	}

	if directoryCountOffset+4 > optionalOffset+optionalSize {
		return peLayout{}, nil, fmt.Errorf("PE optional header is truncated")
	}

	directoryCount := binary.LittleEndian.Uint32(data[directoryCountOffset:])
	directoryBytes := uint64(directoryCount) * 8

	if directoryCount <= peCertificateDirectoryIndex || uint64(directoryOffset)+directoryBytes > uint64(optionalOffset+optionalSize) {
		return peLayout{}, nil, fmt.Errorf("PE data directories are truncated")
	}

	if optionalOffset+64 > len(data) {
		return peLayout{}, nil, fmt.Errorf("PE optional header is truncated")
	}

	layout.sectionAlignment = binary.LittleEndian.Uint32(data[optionalOffset+32:])
	layout.fileAlignment = binary.LittleEndian.Uint32(data[optionalOffset+36:])
	layout.sizeOfHeaders = binary.LittleEndian.Uint32(data[optionalOffset+60:])
	layout.resourceDirectoryPos = directoryOffset + peResourceDirectoryIndex*8
	layout.certificateDirectoryPos = directoryOffset + peCertificateDirectoryIndex*8

	if layout.sectionAlignment == 0 || layout.sectionAlignment&(layout.sectionAlignment-1) != 0 || layout.fileAlignment == 0 || layout.fileAlignment&(layout.fileAlignment-1) != 0 || sectionCount == 0 || sectionCount >= math.MaxUint16 || layout.sizeOfHeaders > uint32(len(data)) {
		return peLayout{}, nil, fmt.Errorf("PE section layout is malformed")
	}

	sectionTableSize := sectionCount * 40
	if layout.sectionTableOffset > len(data)-sectionTableSize {
		return peLayout{}, nil, fmt.Errorf("PE section table is truncated")
	}

	sections := make([]peSection, sectionCount)

	for index := range sections {
		header := data[layout.sectionTableOffset+index*40:]

		sections[index] = peSection{
			virtualSize:    binary.LittleEndian.Uint32(header[8:]),
			virtualAddress: binary.LittleEndian.Uint32(header[12:]),
			rawSize:        binary.LittleEndian.Uint32(header[16:]),
			rawOffset:      binary.LittleEndian.Uint32(header[20:]),
		}

		section := sections[index]
		if section.rawSize != 0 && (section.rawOffset == 0 || uint64(section.rawOffset)+uint64(section.rawSize) > uint64(len(data))) {
			return peLayout{}, nil, fmt.Errorf("PE section data is out of bounds")
		}
	}

	return layout, sections, nil
}

func (parser *peResourceParser) parseNode(relativeOffset uint32) (*peResourceNode, error) {
	if _, exists := parser.active[relativeOffset]; exists {
		return nil, fmt.Errorf("PE resource directory contains a cycle")
	}

	parser.count++
	if parser.count > peResourceLimit {
		return nil, fmt.Errorf("PE resource directory exceeds supported size")
	}

	offset, ok := parser.relativeRange(relativeOffset, 16)
	if !ok {
		return nil, fmt.Errorf("PE resource directory is out of bounds")
	}

	parser.active[relativeOffset] = struct{}{}
	defer delete(parser.active, relativeOffset)

	header := parser.data[offset : offset+16]
	namedCount := int(binary.LittleEndian.Uint16(header[12:]))
	idCount := int(binary.LittleEndian.Uint16(header[14:]))
	childCount := namedCount + idCount

	if childCount > peResourceLimit {
		return nil, fmt.Errorf("PE resource directory exceeds supported size")
	}

	entriesOffset, ok := parser.relativeRange(relativeOffset, 16+childCount*8)
	if !ok {
		return nil, fmt.Errorf("PE resource directory entries are out of bounds")
	}

	entriesOffset += 16

	node := &peResourceNode{
		characteristics: binary.LittleEndian.Uint32(header),
		timestamp:       binary.LittleEndian.Uint32(header[4:]),
		majorVersion:    binary.LittleEndian.Uint16(header[8:]),
		minorVersion:    binary.LittleEndian.Uint16(header[10:]),
		children:        make([]*peResourceChild, 0, childCount),
	}

	for index := range childCount {
		entryData := parser.data[entriesOffset+index*8:]

		nameValue := binary.LittleEndian.Uint32(entryData)
		childValue := binary.LittleEndian.Uint32(entryData[4:])

		identifier, err := parser.parseIdentifier(nameValue)
		if err != nil {
			return nil, err
		}

		child := &peResourceChild{identifier: identifier}

		if childValue&peResourceDirectoryFlag != 0 {
			child.node, err = parser.parseNode(childValue &^ peResourceDirectoryFlag)
		} else {
			child.leaf, err = parser.parseLeaf(childValue)
		}

		if err != nil {
			return nil, err
		}

		node.children = append(node.children, child)
	}

	return node, nil
}

func (parser *peResourceParser) parseIdentifier(value uint32) (peResourceIdentifier, error) {
	if value&peResourceDirectoryFlag == 0 {
		return peResourceIdentifier{id: value}, nil
	}

	offset, ok := parser.relativeRange(value&^peResourceDirectoryFlag, 2)
	if !ok {
		return peResourceIdentifier{}, fmt.Errorf("PE resource name is out of bounds")
	}

	length := int(binary.LittleEndian.Uint16(parser.data[offset:]))

	offset, ok = parser.relativeRange((value&^peResourceDirectoryFlag)+2, length*2)
	if !ok {
		return peResourceIdentifier{}, fmt.Errorf("PE resource name is out of bounds")
	}

	name := make([]uint16, length)

	for index := range name {
		name[index] = binary.LittleEndian.Uint16(parser.data[offset+index*2:])
	}

	return peResourceIdentifier{name: name}, nil
}

func (parser *peResourceParser) parseLeaf(relativeOffset uint32) (*peResourceLeaf, error) {
	offset, ok := parser.relativeRange(relativeOffset, 16)
	if !ok {
		return nil, fmt.Errorf("PE resource data entry is out of bounds")
	}

	entry := parser.data[offset : offset+16]

	rva := binary.LittleEndian.Uint32(entry)
	size := binary.LittleEndian.Uint32(entry[4:])

	dataOffset, ok := peRVAToOffset(rva, size, parser.sections, len(parser.data))
	if !ok {
		return nil, fmt.Errorf("PE resource data is out of bounds")
	}

	leafData := append([]byte(nil), parser.data[dataOffset:dataOffset+int(size)]...)

	return &peResourceLeaf{
		data:     leafData,
		codePage: binary.LittleEndian.Uint32(entry[8:]),
		reserved: binary.LittleEndian.Uint32(entry[12:]),
	}, nil
}

func (parser *peResourceParser) relativeRange(relativeOffset uint32, size int) (int, bool) {
	if size < 0 || uint64(relativeOffset)+uint64(size) > uint64(parser.rootSize) {
		return 0, false
	}

	absolute := parser.rootOffset + int(relativeOffset)
	if absolute < parser.rootOffset || absolute > len(parser.data)-size {
		return 0, false
	}

	return absolute, true
}

func addPEMetadata(root *peResourceNode, entries []Entry) error {
	typeIdentifier := peResourceIdentifier{id: peResourceTypeRCDATA}

	typeChild := findPEResourceChild(root, typeIdentifier)
	if typeChild == nil {
		typeChild = &peResourceChild{
			identifier: typeIdentifier,
			node:       &peResourceNode{},
		}

		root.children = append(root.children, typeChild)
	} else if typeChild.node == nil {
		return fmt.Errorf("PE RT_RCDATA resource tree is malformed")
	}

	typeNode := typeChild.node

	for _, entry := range entries {
		if !utf8.ValidString(entry.Key) {
			return fmt.Errorf("PE resource key %q is not valid UTF-8", entry.Key)
		}

		identifier := peResourceIdentifier{name: utf16.Encode([]rune(entry.Key))}

		nameChild := findPEResourceChild(typeNode, identifier)
		if nameChild == nil {
			nameChild = &peResourceChild{
				identifier: identifier,
				node:       &peResourceNode{},
			}

			typeNode.children = append(typeNode.children, nameChild)
		} else if nameChild.node == nil {
			return fmt.Errorf("PE resource %q tree is malformed", entry.Key)
		}

		languageIdentifier := peResourceIdentifier{id: 0}
		if findPEResourceChild(nameChild.node, languageIdentifier) != nil {
			return fmt.Errorf("PE resource %q already exists", entry.Key)
		}

		nameChild.node.children = append(nameChild.node.children, &peResourceChild{
			identifier: languageIdentifier,
			leaf:       &peResourceLeaf{data: []byte(entry.Value)},
		})
	}

	sortPEResourceTree(root)

	return nil
}

func validatePEResourceTree(node *peResourceNode) error {
	var (
		namedCount int
		idCount    int
	)

	for _, child := range node.children {
		if child.identifier.name != nil {
			namedCount++

			if len(child.identifier.name) > math.MaxUint16 {
				return fmt.Errorf("PE resource name exceeds format limits")
			}
		} else {
			idCount++
		}

		if (child.node == nil) == (child.leaf == nil) {
			return fmt.Errorf("PE resource tree is malformed")
		}

		if child.node != nil {
			err := validatePEResourceTree(child.node)
			if err != nil {
				return err
			}
		}
	}

	if namedCount > math.MaxUint16 || idCount > math.MaxUint16 {
		return fmt.Errorf("PE resource directory exceeds format limits")
	}

	return nil
}

func findPEResourceChild(node *peResourceNode, identifier peResourceIdentifier) *peResourceChild {
	for _, child := range node.children {
		if comparePEResourceIdentifiers(child.identifier, identifier) == 0 {
			return child
		}
	}

	return nil
}

func sortPEResourceTree(node *peResourceNode) {
	sort.SliceStable(node.children, func(left, right int) bool {
		return comparePEResourceIdentifiers(node.children[left].identifier, node.children[right].identifier) < 0
	})

	for _, child := range node.children {
		if child.node != nil {
			sortPEResourceTree(child.node)
		}
	}
}

func comparePEResourceIdentifiers(left, right peResourceIdentifier) int {
	leftNamed := left.name != nil
	rightNamed := right.name != nil

	if leftNamed != rightNamed {
		if leftNamed {
			return -1
		}

		return 1
	}

	if !leftNamed {
		switch {
		case left.id < right.id:
			return -1
		case left.id > right.id:
			return 1
		default:
			return 0
		}
	}

	minimum := min(len(left.name), len(right.name))

	for index := range minimum {
		switch {
		case left.name[index] < right.name[index]:
			return -1
		case left.name[index] > right.name[index]:
			return 1
		}
	}

	switch {
	case len(left.name) < len(right.name):
		return -1
	case len(left.name) > len(right.name):
		return 1
	default:
		return 0
	}
}

func serializePEResources(root *peResourceNode, sectionRVA uint32) ([]byte, error) {
	err := validatePEResourceTree(root)
	if err != nil {
		return nil, err
	}

	cursor := uint64(0)

	assignPEDirectoryOffsets(root, &cursor)
	assignPENameOffsets(root, &cursor)
	assignPELeafOffsets(root, &cursor)

	if cursor > math.MaxUint32 || uint64(sectionRVA)+cursor > math.MaxUint32 {
		return nil, fmt.Errorf("PE resources exceed 32-bit format limits")
	}

	data := make([]byte, int(cursor))
	writePEResourceNode(data, root, sectionRVA)

	return data, nil
}

func assignPEDirectoryOffsets(node *peResourceNode, cursor *uint64) {
	node.offset = uint32(*cursor)
	*cursor += uint64(16 + len(node.children)*8)

	for _, child := range node.children {
		if child.node != nil {
			assignPEDirectoryOffsets(child.node, cursor)
		}
	}
}

func assignPENameOffsets(node *peResourceNode, cursor *uint64) {
	for _, child := range node.children {
		if child.identifier.name != nil {
			child.nameOffset = uint32(*cursor)
			*cursor += uint64(2 + len(child.identifier.name)*2)
		}

		if child.node != nil {
			assignPENameOffsets(child.node, cursor)
		}
	}
}

func assignPELeafOffsets(node *peResourceNode, cursor *uint64) {
	for _, child := range node.children {
		if child.node != nil {
			assignPELeafOffsets(child.node, cursor)

			continue
		}

		*cursor = alignUint64(*cursor, 4)
		child.leaf.dataEntryOffset = uint32(*cursor)
		*cursor += 16
	}

	for _, child := range node.children {
		if child.node != nil {
			continue
		}

		*cursor = alignUint64(*cursor, 4)
		child.leaf.dataOffset = uint32(*cursor)
		*cursor += uint64(len(child.leaf.data))
	}
}

func writePEResourceNode(data []byte, node *peResourceNode, sectionRVA uint32) {
	header := data[node.offset:]

	binary.LittleEndian.PutUint32(header, node.characteristics)
	binary.LittleEndian.PutUint32(header[4:], node.timestamp)
	binary.LittleEndian.PutUint16(header[8:], node.majorVersion)
	binary.LittleEndian.PutUint16(header[10:], node.minorVersion)

	namedCount := 0

	for _, child := range node.children {
		if child.identifier.name != nil {
			namedCount++
		}
	}

	binary.LittleEndian.PutUint16(header[12:], uint16(namedCount))
	binary.LittleEndian.PutUint16(header[14:], uint16(len(node.children)-namedCount))

	for index, child := range node.children {
		entry := header[16+index*8:]

		if child.identifier.name != nil {
			binary.LittleEndian.PutUint32(entry, peResourceDirectoryFlag|child.nameOffset)

			writePEName(data[child.nameOffset:], child.identifier.name)
		} else {
			binary.LittleEndian.PutUint32(entry, child.identifier.id)
		}

		if child.node != nil {
			binary.LittleEndian.PutUint32(entry[4:], peResourceDirectoryFlag|child.node.offset)

			writePEResourceNode(data, child.node, sectionRVA)
		} else {
			binary.LittleEndian.PutUint32(entry[4:], child.leaf.dataEntryOffset)

			writePEResourceLeaf(data, child.leaf, sectionRVA)
		}
	}
}

func writePEName(destination []byte, name []uint16) {
	binary.LittleEndian.PutUint16(destination, uint16(len(name)))

	for index, value := range name {
		binary.LittleEndian.PutUint16(destination[2+index*2:], value)
	}
}

func writePEResourceLeaf(data []byte, leaf *peResourceLeaf, sectionRVA uint32) {
	entry := data[leaf.dataEntryOffset:]

	binary.LittleEndian.PutUint32(entry, sectionRVA+leaf.dataOffset)
	binary.LittleEndian.PutUint32(entry[4:], uint32(len(leaf.data)))
	binary.LittleEndian.PutUint32(entry[8:], leaf.codePage)
	binary.LittleEndian.PutUint32(entry[12:], leaf.reserved)

	copy(data[leaf.dataOffset:], leaf.data)
}

func peRVAToOffset(rva, size uint32, sections []peSection, fileSize int) (int, bool) {
	for _, section := range sections {
		if rva < section.virtualAddress {
			continue
		}

		relative := uint64(rva - section.virtualAddress)
		if relative > uint64(section.rawSize) || uint64(size) > uint64(section.rawSize)-relative {
			continue
		}

		offset := uint64(section.rawOffset) + relative
		if offset > uint64(fileSize) || uint64(size) > uint64(fileSize)-offset {
			return 0, false
		}

		return int(offset), true
	}

	return 0, false
}
