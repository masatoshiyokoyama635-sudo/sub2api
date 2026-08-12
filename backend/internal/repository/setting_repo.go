package repository

import (
	"context"
	"hash/fnv"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/setting"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type settingRepository struct {
	client *ent.Client
}

func NewSettingRepository(client *ent.Client) service.SettingRepository {
	return &settingRepository{client: client}
}

func (r *settingRepository) Get(ctx context.Context, key string) (*service.Setting, error) {
	m, err := r.client.Setting.Query().Where(setting.KeyEQ(key)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, service.ErrSettingNotFound
		}
		return nil, err
	}
	return &service.Setting{
		ID:        m.ID,
		Key:       m.Key,
		Value:     m.Value,
		UpdatedAt: m.UpdatedAt,
	}, nil
}

func (r *settingRepository) GetValue(ctx context.Context, key string) (string, error) {
	setting, err := r.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (r *settingRepository) Set(ctx context.Context, key, value string) error {
	now := time.Now()
	return r.client.Setting.
		Create().
		SetKey(key).
		SetValue(value).
		SetUpdatedAt(now).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
}

func (r *settingRepository) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return map[string]string{}, nil
	}
	settings, err := r.client.Setting.Query().Where(setting.KeyIn(keys...)).All(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	return result, nil
}

func (r *settingRepository) SetMultiple(ctx context.Context, settings map[string]string) error {
	return setMultipleWithClient(ctx, r.client, settings)
}

// UpdateSettingsAtomic serializes administrative settings updates. PostgreSQL
// uses a transaction-scoped advisory lock so the latest security switches can be
// checked and authorized before the single bulk upsert. Non-PostgreSQL callers
// retain the same transaction semantics without issuing a PostgreSQL-only query.
func (r *settingRepository) UpdateSettingsAtomic(ctx context.Context, request service.SettingAtomicUpdate) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := r.updateSettingsAtomicWithClient(ctx, tx.Client(), request); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *settingRepository) updateSettingsAtomicWithClient(ctx context.Context, client *ent.Client, request service.SettingAtomicUpdate) error {
	if client.Driver().Dialect() == dialect.Postgres {
		var rows entsql.Rows
		if err := client.Driver().Query(ctx, "SELECT pg_advisory_xact_lock($1)", []any{settingsUpdateAdvisoryLockKey()}, &rows); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}

	values, err := getMultipleWithClient(ctx, client, []string{
		service.SettingKeyStepUpEnabled,
		service.SettingKeyRiskControlEnabled,
	})
	if err != nil {
		return err
	}
	current := service.SettingSecurityBaseline{
		StepUpEnabled:      values[service.SettingKeyStepUpEnabled] == "true",
		RiskControlEnabled: values[service.SettingKeyRiskControlEnabled] == "true",
	}
	if current != request.Baseline {
		return service.ErrSettingsUpdateConflict
	}
	stepUpDisabled := current.StepUpEnabled && request.Updates[service.SettingKeyStepUpEnabled] == "false"
	riskControlDisabled := current.RiskControlEnabled && request.Updates[service.SettingKeyRiskControlEnabled] == "false"
	if (stepUpDisabled || riskControlDisabled) && request.Authorize == nil {
		return service.ErrSettingsStrictAuthorizationRequired
	}
	if request.Authorize != nil {
		if err := request.Authorize(current); err != nil {
			return err
		}
	}
	return setMultipleWithClient(ctx, client, request.Updates)
}

func settingsUpdateAdvisoryLockKey() int64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte("sub2api:admin-settings-update"))
	return int64(hasher.Sum64())
}

func setMultipleWithClient(ctx context.Context, client *ent.Client, settings map[string]string) error {
	if len(settings) == 0 {
		return nil
	}

	now := time.Now()
	builders := make([]*ent.SettingCreate, 0, len(settings))
	for key, value := range settings {
		builders = append(builders, client.Setting.Create().SetKey(key).SetValue(value).SetUpdatedAt(now))
	}
	return client.Setting.
		CreateBulk(builders...).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
}

func getMultipleWithClient(ctx context.Context, client *ent.Client, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return map[string]string{}, nil
	}
	settings, err := client.Setting.Query().Where(setting.KeyIn(keys...)).All(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(settings))
	for _, item := range settings {
		result[item.Key] = item.Value
	}
	return result, nil
}

func (r *settingRepository) GetAll(ctx context.Context) (map[string]string, error) {
	settings, err := r.client.Setting.Query().All(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	return result, nil
}

func (r *settingRepository) Delete(ctx context.Context, key string) error {
	_, err := r.client.Setting.Delete().Where(setting.KeyEQ(key)).Exec(ctx)
	return err
}
