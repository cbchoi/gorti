package org.gorti.rti;

import hla.rti1516e.AttributeHandle;
import hla.rti1516e.ObjectClassHandle;
import java.nio.file.Files;
import java.nio.file.Path;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.api.io.TempDir;

import static org.junit.jupiter.api.Assertions.*;

@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+")
@Timeout(40)
class JavaAnnexMimSourceDiagnostic20261004Test {
    @TempDir Path temporary;

    @Test
    void suppliedClause11TableSaveNameIsAvailableDespiteAnnexMimDiscrepancy() throws Exception {
        // PDF p298 Table 6 says HLAAnnexSaveName; Annex G XML says HLAnextSaveName.
        // Clause 11.6 makes the table authoritative; preserve XML spelling as an alias.
        try (var session = new JavaAnnexFdd20261004Test.Session()) {
            session.createAndJoin(Files.writeString(temporary.resolve("mim-diagnostic.xml"),
                    JavaAnnexFdd20261004Test.model("")));
            ObjectClassHandle federation = session.rti.getObjectClassHandle("HLAobjectRoot.HLAmanager.HLAfederation");
            assertNotNull(session.rti.getAttributeHandle(federation, "HLAlastSaveName"), "Adjacent table control");
            AttributeHandle tableName = session.rti.getAttributeHandle(federation, "HLAAnnexSaveName");
            assertEquals("HLAAnnexSaveName", session.rti.getAttributeName(federation, tableName));
            assertEquals(tableName, session.rti.getAttributeHandle(federation, "HLAnextSaveName"));
        }
    }
}
