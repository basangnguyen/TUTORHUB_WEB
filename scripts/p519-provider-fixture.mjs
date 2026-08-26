export const P519_PROVIDER_FIXTURE = Object.freeze({
  actorId: "51900000-0000-4000-8000-000000000001",
  classId: "51900000-0000-4000-8000-000000000002",
  tenantId: "51900000-0000-4000-8000-000000000019",
  tenantSlug: "p519-private-alpha-disposable",
  documents: Object.freeze([
    Object.freeze({
      documentId: "51900000-0000-4000-8000-000000000101",
      mediaSpaceId: "51900000-0000-4000-8000-000000000301",
      providerDocumentName: "wb_p519_private_alpha_document_01",
      sessionId: "51900000-0000-4000-8000-000000000201",
    }),
    Object.freeze({
      documentId: "51900000-0000-4000-8000-000000000102",
      mediaSpaceId: "51900000-0000-4000-8000-000000000302",
      providerDocumentName: "wb_p519_private_alpha_document_02",
      sessionId: "51900000-0000-4000-8000-000000000202",
    }),
  ]),
});

export const P519_PROVIDER_DOCUMENTS = Object.freeze(
  P519_PROVIDER_FIXTURE.documents.map(
    (document) => document.providerDocumentName,
  ),
);
