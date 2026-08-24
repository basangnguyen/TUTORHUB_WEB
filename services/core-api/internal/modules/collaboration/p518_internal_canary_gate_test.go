package collaboration

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

func TestP518NonAllowlistedPersistedTrueOverrideHasZeroSideEffects(t *testing.T) {
	allowedTenantID := uuid.MustParse("10000000-0000-4000-8000-000000000018")
	deniedTenantID := uuid.MustParse("10000000-0000-4000-8000-000000000019")
	catalog, err := featurecontrol.NewCatalog(featurecontrol.Guardrails{
		TenantAllowlists: map[featurecontrol.FeatureKey][]uuid.UUID{
			featurecontrol.FeatureClassroomWhiteboards: {allowedTenantID},
		},
	})
	if err != nil {
		t.Fatalf("create P5-18 canary catalog: %v", err)
	}

	documentID := uuid.MustParse("20000000-0000-4000-8000-000000000018")
	spaceID := uuid.MustParse("30000000-0000-4000-8000-000000000018")
	snapshotID := uuid.MustParse("40000000-0000-4000-8000-000000000018")
	access := testAccess()
	access.TenantID = deniedTenantID

	tests := []struct {
		name    string
		attempt func(*Service) error
	}{
		{"grant", func(service *Service) error {
			_, err := service.ExchangeGrant(context.Background(), access, documentID, GrantExchangeInput{
				Capability: CapabilityEdit, ExpectedGeneration: 4,
				ExpectedRevokeGeneration: 4, Origin: "https://app.example.test",
			})
			return err
		}},
		{"lifecycle", func(service *Service) error {
			_, err := service.Suspend(context.Background(), access, documentID, TransitionInput{
				ExpectedVersion: 7, IdempotencyKey: "whiteboard-p518-suspend-0001",
			})
			return err
		}},
		{"snapshot", func(service *Service) error {
			_, err := service.CreateSnapshot(context.Background(), access, documentID, SnapshotCreateInput{
				ExpectedGeneration: 4, IdempotencyKey: "whiteboard-p518-snapshot-0001",
			})
			return err
		}},
		{"export", func(service *Service) error {
			_, err := service.Export(context.Background(), access, documentID, ExportInput{
				ExpectedGeneration: 4, IdempotencyKey: "whiteboard-p518-export-00001",
			})
			return err
		}},
		{"restore", func(service *Service) error {
			_, err := service.Restore(context.Background(), access, documentID, RestoreInput{
				SnapshotID: snapshotID, ExpectedVersion: 7, ExpectedGeneration: 4,
				IdempotencyKey: "whiteboard-p518-restore-0001",
			})
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &p518CanaryRepository{
				catalog: catalog, persistedOverride: true,
				fakeRepository: fakeRepository{
					document: Document{
						ID: documentID, MediaSpaceID: spaceID, Status: DocumentOpen,
						Version: 7, CurrentGeneration: 4, RevokeGeneration: 4,
					},
					policies: defaultCapabilityPolicies(),
				},
			}
			broker := &fakeGrantBroker{}
			workflow := &fakeArtifactWorkflow{}
			service := newTestService(t, repository,
				&fakeSpaceAuthority{space: manageableSpace(spaceID)},
				broker, workflow, uuid.New())

			if err := test.attempt(service); !errors.Is(err, ErrNotFound) {
				t.Fatalf("non-allowlisted %s error=%v, want concealed not found", test.name, err)
			}
			if repository.featureEvaluations != 1 {
				t.Fatalf("non-allowlisted %s evaluations=%d, want 1", test.name, repository.featureEvaluations)
			}
			if repository.getCalls != 0 || repository.createCalls != 0 ||
				repository.grantAuthorityCalls != 0 || repository.transitionCalls != 0 ||
				repository.restoreCalls != 0 {
				t.Fatalf("non-allowlisted %s reached PostgreSQL business adapter", test.name)
			}
			if broker.calls != 0 || len(broker.revoked) != 0 || len(broker.invalidated) != 0 {
				t.Fatalf("non-allowlisted %s reached collaboration runtime", test.name)
			}
			if workflow.snapshotCalls != 0 || workflow.exportCalls != 0 || workflow.restoreCalls != 0 {
				t.Fatalf("non-allowlisted %s reached B2 workflow", test.name)
			}
		})
	}
}

type p518CanaryRepository struct {
	fakeRepository
	catalog             *featurecontrol.Catalog
	persistedOverride   bool
	featureEvaluations  int
	grantAuthorityCalls int
}

func (repository *p518CanaryRepository) Get(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
) (Document, error) {
	repository.featureEvaluations++
	effective, err := repository.catalog.EvaluateFeatureForTenant(
		access.TenantID,
		featurecontrol.FeatureClassroomWhiteboards,
		&repository.persistedOverride,
	)
	if err != nil {
		return Document{}, ErrUnavailable
	}
	if !effective.Enabled {
		return Document{}, ErrNotFound
	}
	return repository.fakeRepository.Get(ctx, access, documentID)
}

func (repository *p518CanaryRepository) GrantAuthority(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
) (GrantAuthority, error) {
	repository.grantAuthorityCalls++
	return repository.fakeRepository.GrantAuthority(ctx, access, documentID)
}
