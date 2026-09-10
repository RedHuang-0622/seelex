package sessionstore

import (
	"context"
	"io/fs"
)

func (repository *jsonRepository) ReadEventRange(_ context.Context, key Key, fromSeq, toSeq uint64) ([]Event, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	if !repository.active(key) {
		return nil, fs.ErrNotExist
	}
	rows, err := repository.layout.readRows(key, fromSeq, toSeq)
	if err != nil {
		return nil, err
	}
	// §5.1 / D13 / S17b：公开读接口原样回传行（commit_id /
	// wire_material / in_out_json 是幂等与装配凭据，不得擦除）。
	return rows, nil
}
