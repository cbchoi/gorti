package mim

// CanonicalAttributeName applies IEEE 1516.1-2010 Clause 11.6: MOM tables
// prevail over Annex G XML. Table 6 spells the next-save label HLAAnnexSaveName.
// Keep the downloaded XML unchanged and normalize only this qualified member.
func CanonicalAttributeName(class, name string) string {
	if class == "HLAobjectRoot.HLAmanager.HLAfederation" && name == "HLAnextSaveName" {
		return "HLAAnnexSaveName"
	}
	return name
}
