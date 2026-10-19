package org.gorti.rti;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.ErrorReadingFDD;
import hla.rti1516e.exceptions.FederationExecutionDoesNotExist;
import java.io.StringReader;
import java.net.URL;
import java.nio.file.*;
import java.util.UUID;
import javax.xml.XMLConstants;
import javax.xml.transform.Source;
import javax.xml.transform.stream.StreamSource;
import javax.xml.validation.SchemaFactory;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.api.io.TempDir;
import org.xml.sax.SAXException;

import static org.junit.jupiter.api.Assertions.*;

@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+")
@Timeout(40)
class JavaAnnexFdd20261004Test {
    @TempDir Path temporary;
    static final String NAMESPACE = "http://standards.ieee.org/IEEE1516-2010";

    static String model(String extension) {
        return "<?xml version=\"1.0\"?><objectModel xmlns=\"" + NAMESPACE + "\">"
                + "<objects><objectClass><name>HLAobjectRoot</name></objectClass></objects>"
                + "<interactions><interactionClass><name>HLAinteractionRoot</name>"
                + "<transportation>HLAreliable</transportation><order>Receive</order>"
                + "<interactionClass><name>LanguageProbe</name><sharing>PublishSubscribe</sharing>"
                + "<transportation>HLAreliable</transportation><order>Receive</order>"
                + "<parameter><name>payload</name></parameter>"
                + "</interactionClass></interactionClass></interactions><dimensions/>"
                + "<transportations><transportation><name>HLAreliable</name><reliable>Yes</reliable>"
                + "</transportation></transportations>"
                + "<switches><autoProvide/><conveyRegionDesignatorSets/>"
                + "<conveyProducingFederate isEnabled=\"true\"/><attributeScopeAdvisory/>"
                + "<attributeRelevanceAdvisory/><objectClassRelevanceAdvisory/>"
                + "<interactionRelevanceAdvisory/><serviceReporting/><exceptionReporting/>"
                + "<delaySubscriptionEvaluation/></switches>" + extension + "</objectModel>";
    }

    @Test
    @EnabledIfSystemProperty(named = "gorti.ieee.fdd.schema", matches = ".+")
    void independentlySchemaValidFddCreatesJoinsAndExposesDeclaredInteraction() throws Exception {
        String xml = model("");
        validate(xml, false);
        try (Session session = new Session()) {
            session.createAndJoin(write(xml));
            InteractionClassHandle interaction = session.rti.getInteractionClassHandle("HLAinteractionRoot.LanguageProbe");
            assertEquals("HLAinteractionRoot.LanguageProbe", session.rti.getInteractionClassName(interaction));
            ParameterHandle parameter = session.rti.getParameterHandle(interaction, "payload");
            assertEquals("payload", session.rti.getParameterName(interaction, parameter));
        }
    }

    @Test
    @EnabledIfSystemProperty(named = "gorti.ieee.fdd.schema", matches = ".+")
    void schemaInvalidSharingEnumerationIsRejectedWithoutCreatingExecution() throws Exception {
        // Unlike an omitted section, an invalid supplied value cannot be filled by the MIM.
        String invalid = model("").replace("<sharing>PublishSubscribe</sharing>", "<sharing>NotASharing</sharing>");
        assertThrows(SAXException.class, () -> validate(invalid, false));
        try (Session session = new Session()) {
            assertThrows(ErrorReadingFDD.class, () -> session.create(write(invalid)));
            assertThrows(FederationExecutionDoesNotExist.class,
                    () -> session.rti.joinFederationExecution("probe", "schema", session.name));
        }
    }

    @Test
    @EnabledIfSystemProperty(named = "gorti.ieee.fdd.schema", matches = ".+")
    void schemaValidForeignNamespaceInformationDoesNotPreventAdmission() throws Exception {
        String extended = model("<audit:review xmlns:audit=\"urn:gorti:test:annex-f\">extension-data</audit:review>");
        validate(extended, true);
        try (Session session = new Session()) {
            session.createAndJoin(write(extended));
            assertNotNull(session.rti.getParameterHandle(
                    session.rti.getInteractionClassHandle("HLAinteractionRoot.LanguageProbe"), "payload"));
        }
    }

    private Path write(String xml) throws Exception {
        return Files.writeString(temporary.resolve(UUID.randomUUID() + ".xml"), xml);
    }

    private static void validate(String xml, boolean extension) throws Exception {
        SchemaFactory factory = SchemaFactory.newInstance(XMLConstants.W3C_XML_SCHEMA_NS_URI);
        factory.setProperty(XMLConstants.ACCESS_EXTERNAL_DTD, "");
        factory.setProperty(XMLConstants.ACCESS_EXTERNAL_SCHEMA, "");
        Source core = new StreamSource(Path.of(System.getProperty("gorti.ieee.fdd.schema")).toFile());
        Source extra = new StreamSource(new StringReader("<xs:schema xmlns:xs=\"http://www.w3.org/2001/XMLSchema\" "
                + "targetNamespace=\"urn:gorti:test:annex-f\" elementFormDefault=\"qualified\">"
                + "<xs:element name=\"review\" type=\"xs:string\"/></xs:schema>"));
        var schema = factory.newSchema(extension ? new Source[]{core, extra} : new Source[]{core});
        schema.newValidator().validate(new StreamSource(new StringReader(xml)));
    }

    static final class Session implements AutoCloseable {
        final RTIambassador rti;
        final String name = "annex-f-" + UUID.randomUUID();
        boolean created;
        boolean joined;

        Session() throws Exception { this(new NullFederateAmbassador()); }
        Session(FederateAmbassador callbacks) throws Exception {
            rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
            rti.connect(callbacks, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
        }
        void create(Path file) throws Exception {
            rti.createFederationExecution(name, new URL[]{file.toUri().toURL()});
            created = true;
        }
        void createAndJoin(Path file) throws Exception {
            create(file);
            rti.joinFederationExecution("java", "annex-test", name);
            joined = true;
        }
        @Override public void close() throws Exception {
            Exception failure = null;
            try { if (joined) rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST); }
            catch (Exception e) { failure = e; }
            try { if (created) rti.destroyFederationExecution(name); }
            catch (Exception e) { if (failure == null) failure = e; else failure.addSuppressed(e); }
            try { rti.disconnect(); }
            catch (Exception e) { if (failure == null) failure = e; else failure.addSuppressed(e); }
            if (failure != null) throw failure;
        }
    }
}
