package metadata

import (
	"bytes"
	"crypto/sha256"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"math"
)

const (
	machoHeaderSize64             = 32
	machoLoadCommandSegment64     = 0x19
	machoLoadCommandCodeSignature = 0x1d
	machoLoadCommandNote          = 0x31
	machoNoteCommandSize          = 40
	machoCodeSignatureCommandSize = 16
	machoCodeSignaturePageBits    = 12
	machoCodeSignaturePageSize    = 1 << machoCodeSignaturePageBits
	machoCodeDirectoryMagic       = 0xfade0c02
	machoEmbeddedSignatureMagic   = 0xfade0cc0
	machoCodeDirectorySlot        = 0
	machoCodeDirectorySize        = 88
	machoSuperBlobSize            = 12
	machoBlobIndexSize            = 8
	machoCodeSignatureSHA256      = 2
	machoCodeSignatureMainBinary  = 1
)

type machoLoadCommand struct {
	offset int
	size   int
	data   []byte
}

type machoSegmentInfo struct {
	commandOffset int
	fileOffset    uint64
	fileSize      uint64
}

type machoCodeSignature struct {
	commandOffset int
	dataOffset    uint32
	dataSize      uint32
}

func embedMachO(data []byte, entries []Entry) ([]byte, error) {
	file, err := macho.NewFile(bytes.NewReader(data))
	if err != nil {
		if isMachOFat(data) {
			return nil, fmt.Errorf("fat Mach-O binaries are not supported for metadata")
		}

		return nil, fmt.Errorf("binary format is not supported: %w", err)
	}

	defer file.Close()

	if file.Magic != macho.Magic64 {
		return nil, fmt.Errorf("only thin 64-bit Mach-O binaries are supported for metadata")
	}

	commands, linkEdit, text, signature, firstDataOffset, err := parseMachOCommands(data, file.ByteOrder, file.Ncmd, file.Cmdsz)
	if err != nil {
		return nil, err
	}

	err = rejectExistingMachONotes(commands, file.ByteOrder, entries)
	if err != nil {
		return nil, err
	}

	unsignedData, err := removeMachOCodeSignature(data, linkEdit, signature, file.ByteOrder)
	if err != nil {
		return nil, err
	}

	commandData := make([]byte, 0, int(file.Cmdsz)+len(entries)*machoNoteCommandSize)
	newLinkEditOffset := 0

	for _, command := range commands {
		if file.ByteOrder.Uint32(command.data) == machoLoadCommandCodeSignature {
			continue
		}

		if command.offset == linkEdit.commandOffset {
			newLinkEditOffset = machoHeaderSize64 + len(commandData)
		}

		commandData = append(commandData, command.data...)
	}

	if newLinkEditOffset == 0 {
		return nil, fmt.Errorf("Mach-O __LINKEDIT command could not be relocated")
	}

	linkEdit.commandOffset = newLinkEditOffset

	result := append([]byte(nil), unsignedData...)
	noteOffsets := make([]uint64, len(entries))

	for index, entry := range entries {
		result = appendZeroPadding(result, 8)

		noteOffsets[index] = uint64(len(result))

		result = append(result, entry.Value...)
	}

	for index, entry := range entries {
		command := make([]byte, machoNoteCommandSize)

		file.ByteOrder.PutUint32(command, machoLoadCommandNote)
		file.ByteOrder.PutUint32(command[4:], machoNoteCommandSize)

		copy(command[8:24], entry.Key)

		file.ByteOrder.PutUint64(command[24:], noteOffsets[index])
		file.ByteOrder.PutUint64(command[32:], uint64(len(entry.Value)))

		commandData = append(commandData, command...)
	}

	requiresAdhocSignature := file.Cpu == macho.CpuArm64
	requiredCommandSpace := len(commandData) + machoCodeSignatureCommandSize

	if machoHeaderSize64+requiredCommandSpace > firstDataOffset {
		return nil, fmt.Errorf("Mach-O load commands need %d bytes but only %d bytes are available", requiredCommandSpace, firstDataOffset-machoHeaderSize64)
	}

	if len(commandData) > math.MaxUint32 {
		return nil, fmt.Errorf("Mach-O load commands exceed format limits")
	}

	commandCount := int(file.Ncmd)

	if signature != nil {
		commandCount--
	}

	commandCount += len(entries)

	if requiresAdhocSignature {
		signatureOffset := alignUint64(uint64(len(result)), 8)
		if signatureOffset > math.MaxUint32 {
			return nil, fmt.Errorf("Mach-O signature offset exceeds format limits")
		}

		signatureSize := machoAdhocSignatureSize(signatureOffset, "a.out")
		if signatureSize > math.MaxUint32 {
			return nil, fmt.Errorf("Mach-O signature size exceeds format limits")
		}

		command := make([]byte, machoCodeSignatureCommandSize)

		file.ByteOrder.PutUint32(command, machoLoadCommandCodeSignature)
		file.ByteOrder.PutUint32(command[4:], machoCodeSignatureCommandSize)
		file.ByteOrder.PutUint32(command[8:], uint32(signatureOffset))
		file.ByteOrder.PutUint32(command[12:], uint32(signatureSize))

		commandData = append(commandData, command...)

		commandCount++

		result = append(result, make([]byte, int(signatureOffset)-len(result))...)
		result = append(result, make([]byte, int(signatureSize))...)
	}

	if commandCount < 0 || uint64(commandCount) > math.MaxUint32 {
		return nil, fmt.Errorf("Mach-O load command count exceeds format limits")
	}

	copy(result[machoHeaderSize64:], commandData)
	clear(result[machoHeaderSize64+len(commandData) : firstDataOffset])

	file.ByteOrder.PutUint32(result[16:], uint32(commandCount))
	file.ByteOrder.PutUint32(result[20:], uint32(len(commandData)))

	err = updateMachOLinkEdit(result, linkEdit, uint64(len(result)), file.ByteOrder)
	if err != nil {
		return nil, err
	}

	if requiresAdhocSignature {
		signatureCommand := commandData[len(commandData)-machoCodeSignatureCommandSize:]

		signatureOffset := file.ByteOrder.Uint32(signatureCommand[8:])
		signatureSize := file.ByteOrder.Uint32(signatureCommand[12:])

		signatureData := result[int(signatureOffset) : int(signatureOffset)+int(signatureSize)]

		writeMachOAdhocSignature(signatureData, result[:signatureOffset], "a.out", uint64(signatureOffset), text.fileOffset, text.fileSize, file.Type == macho.TypeExec)
	}

	return result, nil
}

func parseMachOCommands(data []byte, byteOrder binary.ByteOrder, commandCount, commandSize uint32) ([]machoLoadCommand, machoSegmentInfo, machoSegmentInfo, *machoCodeSignature, int, error) {
	if uint64(machoHeaderSize64)+uint64(commandSize) > uint64(len(data)) {
		return nil, machoSegmentInfo{}, machoSegmentInfo{}, nil, 0, fmt.Errorf("Mach-O load commands are truncated")
	}

	commands := make([]machoLoadCommand, 0, commandCount)
	offset := machoHeaderSize64
	commandEnd := offset + int(commandSize)
	firstDataOffset := len(data)

	var (
		linkEdit  machoSegmentInfo
		text      machoSegmentInfo
		signature *machoCodeSignature
	)

	for range commandCount {
		if offset > commandEnd-8 {
			return nil, machoSegmentInfo{}, machoSegmentInfo{}, nil, 0, fmt.Errorf("Mach-O load command is truncated")
		}

		size := int(byteOrder.Uint32(data[offset+4:]))
		if size < 8 || size%8 != 0 || size > commandEnd-offset {
			return nil, machoSegmentInfo{}, machoSegmentInfo{}, nil, 0, fmt.Errorf("Mach-O load command has invalid size")
		}

		commandData := data[offset : offset+size]
		command := machoLoadCommand{offset: offset, size: size, data: commandData}

		commands = append(commands, command)

		switch byteOrder.Uint32(commandData) {
		case machoLoadCommandSegment64:
			if size < 72 {
				return nil, machoSegmentInfo{}, machoSegmentInfo{}, nil, 0, fmt.Errorf("Mach-O segment command is truncated")
			}

			segment := machoSegmentInfo{
				commandOffset: offset,
				fileOffset:    byteOrder.Uint64(commandData[40:]),
				fileSize:      byteOrder.Uint64(commandData[48:]),
			}

			name := string(bytes.TrimRight(commandData[8:24], "\x00"))

			switch name {
			case "__LINKEDIT":
				if linkEdit.commandOffset != 0 {
					return nil, machoSegmentInfo{}, machoSegmentInfo{}, nil, 0, fmt.Errorf("Mach-O has multiple __LINKEDIT segments")
				}

				linkEdit = segment
			case "__TEXT":
				text = segment
			}

			sectionCount := int(byteOrder.Uint32(commandData[64:]))
			if sectionCount > (size-72)/80 {
				return nil, machoSegmentInfo{}, machoSegmentInfo{}, nil, 0, fmt.Errorf("Mach-O segment sections are truncated")
			}

			for sectionIndex := range sectionCount {
				sectionOffset := 72 + sectionIndex*80

				fileOffset := int(byteOrder.Uint32(commandData[sectionOffset+48:]))
				sectionSize := byteOrder.Uint64(commandData[sectionOffset+40:])

				if fileOffset != 0 && sectionSize != 0 && fileOffset < firstDataOffset {
					firstDataOffset = fileOffset
				}
			}
		case machoLoadCommandCodeSignature:
			if size != machoCodeSignatureCommandSize || signature != nil {
				return nil, machoSegmentInfo{}, machoSegmentInfo{}, nil, 0, fmt.Errorf("Mach-O code signature command is malformed")
			}

			signature = &machoCodeSignature{
				commandOffset: offset,
				dataOffset:    byteOrder.Uint32(commandData[8:]),
				dataSize:      byteOrder.Uint32(commandData[12:]),
			}
		}

		offset += size
	}

	if offset != commandEnd {
		return nil, machoSegmentInfo{}, machoSegmentInfo{}, nil, 0, fmt.Errorf("Mach-O load command size is inconsistent")
	}

	if linkEdit.commandOffset == 0 || text.commandOffset == 0 {
		return nil, machoSegmentInfo{}, machoSegmentInfo{}, nil, 0, fmt.Errorf("Mach-O is missing required segments")
	}

	if firstDataOffset == len(data) || firstDataOffset < commandEnd {
		return nil, machoSegmentInfo{}, machoSegmentInfo{}, nil, 0, fmt.Errorf("Mach-O load command space is malformed")
	}

	return commands, linkEdit, text, signature, firstDataOffset, nil
}

func rejectExistingMachONotes(commands []machoLoadCommand, byteOrder binary.ByteOrder, entries []Entry) error {
	for _, command := range commands {
		if byteOrder.Uint32(command.data) != machoLoadCommandNote {
			continue
		}

		if command.size != machoNoteCommandSize {
			return fmt.Errorf("Mach-O LC_NOTE command is malformed")
		}

		for _, entry := range entries {
			var owner [16]byte

			copy(owner[:], entry.Key)

			if bytes.Equal(command.data[8:24], owner[:]) {
				return fmt.Errorf("Mach-O note %q already exists", entry.Key)
			}
		}
	}

	return nil
}

func removeMachOCodeSignature(data []byte, linkEdit machoSegmentInfo, signature *machoCodeSignature, byteOrder binary.ByteOrder) ([]byte, error) {
	if signature == nil {
		return append([]byte(nil), data...), nil
	}

	signatureEnd := uint64(signature.dataOffset) + uint64(signature.dataSize)
	linkEditEnd := linkEdit.fileOffset + linkEdit.fileSize

	if signature.dataSize == 0 || signatureEnd != uint64(len(data)) || signatureEnd > linkEditEnd || linkEditEnd-signatureEnd > 16 {
		return nil, fmt.Errorf("Mach-O code signature is not a valid final __LINKEDIT payload")
	}

	result := append([]byte(nil), data[:signature.dataOffset]...)

	err := updateMachOLinkEdit(result, linkEdit, uint64(len(result)), byteOrder)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func updateMachOLinkEdit(data []byte, linkEdit machoSegmentInfo, end uint64, byteOrder binary.ByteOrder) error {
	if end < linkEdit.fileOffset || linkEdit.commandOffset > len(data)-56 {
		return fmt.Errorf("Mach-O __LINKEDIT bounds are malformed")
	}

	fileSize := end - linkEdit.fileOffset
	memorySize := alignUint64(fileSize, 4096)

	byteOrder.PutUint64(data[linkEdit.commandOffset+32:], memorySize)
	byteOrder.PutUint64(data[linkEdit.commandOffset+48:], fileSize)

	return nil
}

func machoAdhocSignatureSize(codeSize uint64, identifier string) uint64 {
	hashCount := (codeSize + machoCodeSignaturePageSize - 1) / machoCodeSignaturePageSize

	return machoSuperBlobSize + machoBlobIndexSize + machoCodeDirectorySize + uint64(len(identifier)+1) + hashCount*sha256.Size
}

func writeMachOAdhocSignature(destination, code []byte, identifier string, codeSize, textOffset, textSize uint64, mainBinary bool) {
	hashCount := (codeSize + machoCodeSignaturePageSize - 1) / machoCodeSignaturePageSize
	codeDirectoryOffset := machoSuperBlobSize + machoBlobIndexSize
	identifierOffset := machoCodeDirectorySize
	hashOffset := identifierOffset + len(identifier) + 1

	binary.BigEndian.PutUint32(destination, machoEmbeddedSignatureMagic)
	binary.BigEndian.PutUint32(destination[4:], uint32(len(destination)))
	binary.BigEndian.PutUint32(destination[8:], 1)
	binary.BigEndian.PutUint32(destination[12:], machoCodeDirectorySlot)
	binary.BigEndian.PutUint32(destination[16:], uint32(codeDirectoryOffset))

	codeDirectory := destination[codeDirectoryOffset:]

	binary.BigEndian.PutUint32(codeDirectory, machoCodeDirectoryMagic)
	binary.BigEndian.PutUint32(codeDirectory[4:], uint32(len(destination)-codeDirectoryOffset))
	binary.BigEndian.PutUint32(codeDirectory[8:], 0x20400)
	binary.BigEndian.PutUint32(codeDirectory[12:], 0x20002)
	binary.BigEndian.PutUint32(codeDirectory[16:], uint32(hashOffset))
	binary.BigEndian.PutUint32(codeDirectory[20:], uint32(identifierOffset))
	binary.BigEndian.PutUint32(codeDirectory[28:], uint32(hashCount))
	binary.BigEndian.PutUint32(codeDirectory[32:], uint32(codeSize))

	codeDirectory[36] = sha256.Size
	codeDirectory[37] = machoCodeSignatureSHA256
	codeDirectory[39] = machoCodeSignaturePageBits

	binary.BigEndian.PutUint64(codeDirectory[64:], textOffset)
	binary.BigEndian.PutUint64(codeDirectory[72:], textSize)

	if mainBinary {
		binary.BigEndian.PutUint64(codeDirectory[80:], machoCodeSignatureMainBinary)
	}

	copy(codeDirectory[identifierOffset:], identifier)

	hashes := codeDirectory[hashOffset:]

	for pageOffset := 0; pageOffset < len(code); pageOffset += machoCodeSignaturePageSize {
		pageEnd := min(pageOffset+machoCodeSignaturePageSize, len(code))

		digest := sha256.Sum256(code[pageOffset:pageEnd])
		copy(hashes, digest[:])

		hashes = hashes[sha256.Size:]
	}
}

func isMachOFat(data []byte) bool {
	if len(data) < 4 {
		return false
	}

	magic := binary.BigEndian.Uint32(data)

	return magic == macho.MagicFat || magic == 0xbebafeca || magic == 0xcafebabf || magic == 0xbfbafeca
}
