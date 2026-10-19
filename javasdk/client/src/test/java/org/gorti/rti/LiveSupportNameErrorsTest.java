package org.gorti.rti;

import hla.rti1516e.RTIambassador;
import hla.rti1516e.exceptions.InvalidInteractionClassHandle;
import hla.rti1516e.exceptions.InvalidObjectClassHandle;
import hla.rti1516e.exceptions.NameNotFound;
import java.nio.file.Files;
import java.nio.file.Path;
import org.gorti.rti.types.Handles;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.api.io.TempDir;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

import static org.junit.jupiter.api.Assertions.*;

@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+")
@Timeout(40)
class LiveSupportNameErrorsTest {
    @TempDir Path temporary;

    @ParameterizedTest
    @ValueSource(strings = {"object", "attribute", "interaction", "parameter", "dimension"})
    void missingNamesHaveExactStandardExceptionsAndPreserveLookups(String service) throws Exception {
        try (var session = new JavaAnnexFdd20261004Test.Session()) {
            session.createAndJoin(Files.writeString(temporary.resolve("lookup.xml"), JavaAnnexFdd20261004Test.model("")));
            Object before = lookup(session.rti, service, false);
            NameNotFound error = assertThrows(NameNotFound.class, () -> lookup(session.rti, service, true));
            assertEquals(NameNotFound.class, error.getClass());
            assertEquals(before, lookup(session.rti, service, false), "Rejected lookup must not alter the FDD");
        }
    }

    @ParameterizedTest
    @ValueSource(longs = {0, Long.MAX_VALUE})
    void invalidObjectClassDoesNotBecomeMissingAttribute(long handle) throws Exception {
        try (var session = new JavaAnnexFdd20261004Test.Session()) {
            session.createAndJoin(Files.writeString(temporary.resolve("lookup.xml"), JavaAnnexFdd20261004Test.model("")));
            assertThrows(InvalidObjectClassHandle.class,
                    () -> session.rti.getAttributeHandle(Handles.objectClass(handle), "HLAlastSaveName"));
            assertNotNull(lookup(session.rti, "attribute", false));
        }
    }

    @ParameterizedTest
    @ValueSource(longs = {0, Long.MAX_VALUE})
    void invalidInteractionClassDoesNotBecomeMissingParameter(long handle) throws Exception {
        try (var session = new JavaAnnexFdd20261004Test.Session()) {
            session.createAndJoin(Files.writeString(temporary.resolve("lookup.xml"), JavaAnnexFdd20261004Test.model("")));
            assertThrows(InvalidInteractionClassHandle.class,
                    () -> session.rti.getParameterHandle(Handles.interactionClass(handle), "payload"));
            assertNotNull(lookup(session.rti, "parameter", false));
        }
    }

    private static Object lookup(RTIambassador rti, String service, boolean missing) throws Exception {
        String absent = "DefinitelyAbsentLookupProbe";
        switch (service) {
            case "object": return rti.getObjectClassHandle(missing ? absent : "HLAobjectRoot.HLAmanager.HLAfederation");
            case "attribute": return rti.getAttributeHandle(rti.getObjectClassHandle("HLAobjectRoot.HLAmanager.HLAfederation"), missing ? absent : "HLAlastSaveName");
            case "interaction": return rti.getInteractionClassHandle(missing ? absent : "HLAinteractionRoot.LanguageProbe");
            case "parameter": return rti.getParameterHandle(rti.getInteractionClassHandle("HLAinteractionRoot.LanguageProbe"), missing ? absent : "payload");
            case "dimension": return rti.getDimensionHandle(missing ? absent : "HLAfederate");
            default: throw new AssertionError(service);
        }
    }
}
