package persistence

import (
    "encoding/binary"
    "encoding/json"
    "errors"
    "hash/crc32"
    "io"
    "log"
    "os"
    "path/filepath"
    "sync"
)

const walMagic uint32 = 0xDEADCAFE

type WAL struct {
    // mu serializes all writes and truncations
    mu      sync.Mutex
    dataDir string
    file    *os.File
}

func NewWAL(dataDir string) (*WAL, error) {
    if err := os.MkdirAll(dataDir, 0755); err != nil {
        return nil, err
    }
    path := filepath.Join(dataDir, "wal.log")
    f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
    if err != nil {
        return nil, err
    }
    return &WAL{dataDir: dataDir, file: f}, nil
}

// Append writes a record to the WAL and fsyncs before returning.
// Record format: [4 magic][4 length][N data][4 crc32]
// NEVER returns without fsyncing — this is the durability guarantee.
func (w *WAL) Append(data []byte) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    checksum := crc32.ChecksumIEEE(data)

    buf := make([]byte, 0, 8+len(data)+4)

    // Header: magic + length
    var header [8]byte
    binary.BigEndian.PutUint32(header[0:4], walMagic)
    binary.BigEndian.PutUint32(header[4:8], uint32(len(data)))
    buf = append(buf, header[:]...)

    // Data
    buf = append(buf, data...)

    // Footer: crc32
    var footer [4]byte
    binary.BigEndian.PutUint32(footer[0:4], checksum)
    buf = append(buf, footer[:]...)

    if _, err := w.file.Write(buf); err != nil {
        return err
    }

    // fsync — NEVER skip this
    return w.file.Sync()
}

func (w *WAL) readAllLocked() ([][]byte, error) {
    if _, err := w.file.Seek(0, io.SeekStart); err != nil {
        return nil, err
    }

    var records [][]byte

    for {
        // Read header
        var header [8]byte
        if _, err := io.ReadFull(w.file, header[:]); err != nil {
            if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
                break // clean end or partial header = stop
            }
            return nil, err
        }

        magic  := binary.BigEndian.Uint32(header[0:4])
        length := binary.BigEndian.Uint32(header[4:8])

        if magic != walMagic {
            break // corrupted record — stop reading
        }

        // Read data
        data := make([]byte, length)
        if _, err := io.ReadFull(w.file, data); err != nil {
            break // partial data = corrupted tail, stop
        }

        // Read footer
        var footer [4]byte
        if _, err := io.ReadFull(w.file, footer[:]); err != nil {
            break // partial footer = corrupted tail, stop
        }

        expected := binary.BigEndian.Uint32(footer[0:4])
        actual   := crc32.ChecksumIEEE(data)
        if expected != actual {
            break // checksum mismatch = corrupted tail, stop
        }

        records = append(records, data)
    }

    return records, nil
}

// ReadAll reads all valid records from the WAL in order.
// Stops at the first corrupted or incomplete record — treats it as end of log.
// Safe to call after a crash — partial trailing writes are silently ignored.
func (w *WAL) ReadAll() ([][]byte, error) {
    w.mu.Lock()
    defer w.mu.Unlock()
    return w.readAllLocked()
}

// TruncateFromIndex rewrites the WAL keeping only records with an Index strictly less than the given index.
// Used when a follower detects conflicting entries from the leader (Truncate After).
func (w *WAL) TruncateFromIndex(index uint64) error {
	return w.filterLocked(func(entryIndex uint64) bool {
		return entryIndex < index
	})
}

// DiscardBeforeIndex rewrites the WAL keeping only records with an Index greater than or equal to the given index.
// Used for log compaction after a snapshot (Truncate Before).
func (w *WAL) DiscardBeforeIndex(index uint64) error {
	return w.filterLocked(func(entryIndex uint64) bool {
		return entryIndex >= index
	})
}

func (w *WAL) filterLocked(keepFn func(uint64) bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	all, err := w.readAllLocked()
	if err != nil {
		return err
	}

	var keep [][]byte
	for _, record := range all {
		var entry struct {
			Index uint64 `json:"index"`
		}
		if err := json.Unmarshal(record, &entry); err != nil {
			return err // corrupted JSON
		}
		if keepFn(entry.Index) {
			keep = append(keep, record)
		} else {
			log.Printf("[WAL] Discarding entry at index %d", entry.Index)
		}
	}
	log.Printf("[WAL] Filtered: kept %d records, discarded %d", len(keep), len(all)-len(keep))

	if len(keep) == len(all) {
		return nil // nothing to do
	}

	// Write to temp file
	tmpPath := filepath.Join(w.dataDir, "wal.tmp")
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	for _, record := range keep {
		checksum := crc32.ChecksumIEEE(record)
		var header [8]byte
		binary.BigEndian.PutUint32(header[0:4], walMagic)
		binary.BigEndian.PutUint32(header[4:8], uint32(len(record)))
		var footer [4]byte
		binary.BigEndian.PutUint32(footer[0:4], checksum)

		tmp.Write(header[:])
		tmp.Write(record)
		tmp.Write(footer[:])
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()

	// Close original file before renaming on Windows
	w.file.Close()

	// Atomic rename
	walPath := filepath.Join(w.dataDir, "wal.log")
	if err := os.Rename(tmpPath, walPath); err != nil {
		return err
	}

	// Reopen file handle
	var openErr error
	w.file, openErr = os.OpenFile(walPath, os.O_RDWR|os.O_APPEND, 0644)
	return openErr
}

func (w *WAL) Close() error {
    w.mu.Lock()
    defer w.mu.Unlock()
    return w.file.Close()
}