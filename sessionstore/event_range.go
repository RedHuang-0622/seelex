package sessionstore

import (
	"context"
	"errors"
	"io/fs"
)

// selectEventRange 按 EventSeq 范围（含端点）过滤事件，保持原有顺序。
// from > to 返回显式错误，不允许倒置范围（interfaces.md §Event 模块）。
func selectEventRange(events []Event, fromSeq, toSeq uint64) ([]Event, error) {
	if fromSeq > toSeq {
		return nil, errors.New("session storage: invalid event range")
	}
	selected := make([]Event, 0)
	for _, event := range events {
		if event.Seq >= fromSeq && event.Seq <= toSeq {
			selected = append(selected, event)
		}
	}
	return selected, nil
}

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

func (repository *sqlRepository) ReadEventRange(ctx context.Context, key Key, fromSeq, toSeq uint64) ([]Event, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	events, err := repository.readAllEvents(ctx, key)
	if err != nil {
		return nil, err
	}
	return selectEventRange(events, fromSeq, toSeq)
}

func (repository *redisRepository) ReadEventRange(ctx context.Context, key Key, fromSeq, toSeq uint64) ([]Event, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	manifest, err := repository.readManifest(ctx, key)
	if err != nil {
		return nil, err
	}
	events, err := repository.readEvents(ctx, key, manifest)
	if err != nil {
		return nil, err
	}
	return selectEventRange(events, fromSeq, toSeq)
}
