package gameidx

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const RootParentID uint64 = 0xdbb1a1d1b108b927

type Resource struct {
	ID, ParentID uint64
	Name         string
}

type File struct {
	ResourceID, VolumeID, Offset, Compression uint64
	Size, UnpackedSize, CRC32                 uint32
}

type Volume struct {
	ID   uint64
	Name string
}

type Index struct {
	Resources []Resource
	Files     []File
	Volumes   []Volume
}

// Parse reads the two known World of Warships IDX layouts.
func Parse(data []byte) (index Index, err error) {
	var (
		version, resourceCount, fileCount, volumeCount uint32
		resourceOffset, fileOffset, volumeOffset       uint64
		resourceSize, volumeSize                       uint64
		entry                                          []byte
	)

	if len(data) < 40 || binary.LittleEndian.Uint32(data[:4]) != 0x50465349 {
		err = fmt.Errorf("invalid IDX header")
		return
	}

	version = binary.LittleEndian.Uint32(data[4:8])
	switch version {
	case 0x02000000:
		if len(data) < 56 {
			err = fmt.Errorf("truncated IDX v40 metadata")
			return
		}

		resourceCount = binary.LittleEndian.Uint32(data[16:20])
		fileCount = binary.LittleEndian.Uint32(data[20:24])
		volumeCount = binary.LittleEndian.Uint32(data[24:28])
		resourceOffset = 16 + binary.LittleEndian.Uint64(data[32:40])
		fileOffset = 16 + binary.LittleEndian.Uint64(data[40:48])
		volumeOffset = 16 + binary.LittleEndian.Uint64(data[48:56])
		resourceSize, volumeSize = 32, 24
	case 0x01010004:
		resourceCount = binary.LittleEndian.Uint32(data[16:20])
		resourceOffset = 16 + uint64(binary.LittleEndian.Uint32(data[20:24]))
		fileCount = binary.LittleEndian.Uint32(data[24:28])
		fileOffset = 16 + uint64(binary.LittleEndian.Uint32(data[28:32]))
		volumeCount = binary.LittleEndian.Uint32(data[32:36])
		volumeOffset = 16 + uint64(binary.LittleEndian.Uint32(data[36:40]))
		resourceSize, volumeSize = 24, 16
	default:
		err = fmt.Errorf("unsupported IDX version 0x%08x", version)
		return
	}

	if _, err = table(data, resourceOffset, uint64(resourceCount), resourceSize); err != nil {
		return
	}

	if _, err = table(data, fileOffset, uint64(fileCount), 48); err != nil {
		return
	}

	if _, err = table(data, volumeOffset, uint64(volumeCount), volumeSize); err != nil {
		return
	}

	index.Resources = make([]Resource, 0, resourceCount)
	for i := range uint64(resourceCount) {
		entry = data[resourceOffset+i*resourceSize : resourceOffset+(i+1)*resourceSize]

		var (
			resource               Resource
			nameOffset, nameLength uint64
		)

		if version == 0x02000000 {
			resource.ID = binary.LittleEndian.Uint64(entry[16:24])
			resource.ParentID = binary.LittleEndian.Uint64(entry[24:32])
			nameOffset = resourceOffset + i*resourceSize + binary.LittleEndian.Uint64(entry[8:16])
		} else {
			resource.ID = binary.LittleEndian.Uint64(entry[:8])
			resource.ParentID = binary.LittleEndian.Uint64(entry[8:16])
			nameLength = uint64(binary.LittleEndian.Uint32(entry[16:20]))
			nameOffset = resourceOffset + i*resourceSize + 16 + uint64(binary.LittleEndian.Uint32(entry[20:24]))
		}

		if resource.Name, err = name(data, nameOffset, nameLength); err != nil {
			err = fmt.Errorf("resource %d: %w", i, err)
			return
		}

		index.Resources = append(index.Resources, resource)
	}

	index.Files = make([]File, 0, fileCount)
	for i := range uint64(fileCount) {
		entry = data[fileOffset+i*48 : fileOffset+(i+1)*48]

		var file File

		if version == 0x02000000 {
			file.ResourceID = binary.LittleEndian.Uint64(entry[:8])
			file.VolumeID = binary.LittleEndian.Uint64(entry[8:16])
			file.Offset = binary.LittleEndian.Uint64(entry[16:24])
			file.Compression = binary.LittleEndian.Uint64(entry[24:32])
			file.Size = binary.LittleEndian.Uint32(entry[32:36])
			file.CRC32 = binary.LittleEndian.Uint32(entry[36:40])
			file.UnpackedSize = binary.LittleEndian.Uint32(entry[40:44])
		} else {
			file.Offset = binary.LittleEndian.Uint64(entry[:8])
			file.Size = binary.LittleEndian.Uint32(entry[12:16])
			file.CRC32 = binary.LittleEndian.Uint32(entry[16:20])
			file.UnpackedSize = binary.LittleEndian.Uint32(entry[20:24])
			file.Compression = binary.LittleEndian.Uint64(entry[24:32])
			file.ResourceID = binary.LittleEndian.Uint64(entry[32:40])
			file.VolumeID = binary.LittleEndian.Uint64(entry[40:48])
		}

		index.Files = append(index.Files, file)
	}

	index.Volumes = make([]Volume, 0, volumeCount)
	for i := uint64(0); i < uint64(volumeCount); i++ {
		entry = data[volumeOffset+i*volumeSize : volumeOffset+(i+1)*volumeSize]

		var (
			volume                 Volume
			nameOffset, nameLength uint64
		)

		if version == 0x02000000 {
			volume.ID = binary.LittleEndian.Uint64(entry[16:24])
			nameOffset = volumeOffset + i*volumeSize + binary.LittleEndian.Uint64(entry[8:16])
		} else {
			volume.ID = binary.LittleEndian.Uint64(entry[:8])
			nameLength = uint64(binary.LittleEndian.Uint32(entry[8:12]))
			nameOffset = volumeOffset + i*volumeSize + 8 + uint64(binary.LittleEndian.Uint32(entry[12:16]))
		}

		if volume.Name, err = name(data, nameOffset, nameLength); err != nil {
			err = fmt.Errorf("volume %d: %w", i, err)
			return
		}

		volume.Name = string(bytes.TrimPrefix([]byte(volume.Name), []byte("//.//")))
		index.Volumes = append(index.Volumes, volume)
	}

	return
}

// table checks that all entries fit within the IDX data.
func table(data []byte, offset, count, size uint64) (entries []byte, err error) {
	if size == 0 || offset > uint64(len(data)) || count > (uint64(len(data))-offset)/size {
		err = fmt.Errorf("IDX table outside file")
		return
	}

	entries = data[offset : offset+count*size]
	return
}

// name reads a null-terminated or fixed-width IDX name.
func name(data []byte, offset, length uint64) (value string, err error) {
	var (
		raw []byte
		end int
	)

	if offset >= uint64(len(data)) || length > uint64(len(data))-offset {
		err = fmt.Errorf("IDX name outside file")
		return
	}

	raw = data[offset:]

	if length != 0 {
		raw = raw[:length]
	} else if end = bytes.IndexByte(raw, 0); end >= 0 {
		raw = raw[:end]
	} else {
		err = fmt.Errorf("unterminated IDX name")
		return
	}

	if end = bytes.IndexByte(raw, 0); end >= 0 {
		raw = raw[:end]
	}

	value = string(raw)
	return
}
