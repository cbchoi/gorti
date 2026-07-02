// Package m36spec — M36 DD-2 specification tests: MOM object-instance
// fan-out via the standard object-registry path (IEEE 1516-2010 §10 /
// 1516.1-2010 §11).
//
// Contract under test: the RTI registers one HLAmanager.HLAfederation
// instance per federation and one HLAmanager.HLAfederate instance per
// joined federate, delivering Discover / Reflect / Remove to
// subscribers through the STANDARD object-management callbacks — no
// bespoke MOM API (conformance fixture mom_federation_lifecycle,
// catalogue 16.1). Late subscribers to the MOM classes receive
// retroactive Discover+Reflect for already-existing instances via the
// declaration manager's post-subscribe hook.
package m36spec
