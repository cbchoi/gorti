package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.ByteString;
import com.google.protobuf.Message;
import hla.rti1516e.*;
import hla.rti1516e.encoding.ByteWrapper;
import hla.rti1516e.exceptions.*;
import io.grpc.*;
import io.grpc.stub.MetadataUtils;
import java.lang.reflect.Field;
import java.util.ArrayList;
import java.util.Collection;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import org.gorti.rti.types.Handles;
import org.gorti.rti.types.TimeValues;
import org.junit.jupiter.api.Test;
import rti.v1.Common;
import rti.v1.Declaration;
import rti.v1.Federation;
import rti.v1.Support;

class ExchangeServicesTest {
    private final RecordingChannel wire = new RecordingChannel();
    private final Context context = new Context(wire);
    private final ExchangeServices exchange = new ExchangeServices(context);
    private final ObjectClassHandle objectClass = Handles.objectClass(21);
    private final ObjectInstanceHandle object = Handles.objectInstance(31);
    private final InteractionClassHandle interaction = Handles.interactionClass(41);

    @Test
    void checkpointRestoresProvisionalObjectsWithoutAliasingOrReusingRetractionIds() throws Exception {
        discover();
        context.regulating = true;
        exchange.removed(31, true, 8, 1, 20);
        ExchangeServices.State saved = exchange.checkpoint();
        exchange.constrainDisabled();
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
        exchange.restore(saved, 100);
        assertEquals("car", exchange.getObjectInstanceName(object));
        assertEquals(100, exchange.allocateRetraction());
        exchange.advanceKnowledge(20);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
        exchange.restore(saved, 50);
        assertEquals("car", exchange.getObjectInstanceName(object));
        assertEquals(101, exchange.allocateRetraction());
        exchange.retracted(8, 1);
        exchange.advanceKnowledge(20);
        assertEquals("car", exchange.getObjectInstanceName(object));
    }

    @Test
    void everyDeclarationOverloadUsesItsWireOperationAndOptions() throws Exception {
        AttributeSet attributes = attributes();
        exchange.publishObjectClassAttributes(objectClass, attributes);
        assertEquals(List.of(51L), last(Declaration.PubObjAttrsRequest.class).getAttributeHandlesList());
        exchange.unpublishObjectClassAttributes(objectClass, attributes);
        assertEquals(List.of(51L), last(Declaration.UnpubObjAttrsRequest.class).getAttributeHandlesList());
        assertTrue(last(Declaration.UnpubObjAttrsRequest.class).hasAttributeSetPresent());
        assertTrue(last(Declaration.UnpubObjAttrsRequest.class).getAttributeSetPresent());
        exchange.unpublishObjectClass(objectClass);
        assertEquals(0, last(Declaration.UnpubObjAttrsRequest.class).getAttributeHandlesCount());
        assertFalse(last(Declaration.UnpubObjAttrsRequest.class).hasAttributeSetPresent());
        exchange.subscribeObjectClassAttributes(objectClass, attributes);
        assertSubscription(true, "");
        exchange.subscribeObjectClassAttributes(objectClass, attributes, "fast");
        assertSubscription(true, "fast");
        exchange.subscribeObjectClassAttributesPassively(objectClass, attributes);
        assertSubscription(false, "");
        exchange.subscribeObjectClassAttributesPassively(objectClass, attributes, "slow");
        assertSubscription(false, "slow");
        exchange.unsubscribeObjectClassAttributes(objectClass, attributes);
        assertEquals(List.of(51L), last(Declaration.UnsubObjAttrsRequest.class).getAttributeHandlesList());
        assertTrue(last(Declaration.UnsubObjAttrsRequest.class).hasAttributeSetPresent());
        assertTrue(last(Declaration.UnsubObjAttrsRequest.class).getAttributeSetPresent());
        exchange.unsubscribeObjectClass(objectClass);
        assertEquals(0, last(Declaration.UnsubObjAttrsRequest.class).getAttributeHandlesCount());
        assertFalse(last(Declaration.UnsubObjAttrsRequest.class).hasAttributeSetPresent());
        exchange.publishInteractionClass(interaction);
        assertEquals(41, last(Declaration.PubInterRequest.class).getInteractionClassHandle());
        exchange.unpublishInteractionClass(interaction);
        assertEquals(41, last(Declaration.UnpubInterRequest.class).getInteractionClassHandle());
        exchange.subscribeInteractionClass(interaction);
        assertTrue(last(Declaration.SubInterRequest.class).getActive());
        exchange.subscribeInteractionClassPassively(interaction);
        assertTrue(last(Declaration.SubInterRequest.class).hasActive());
        assertFalse(last(Declaration.SubInterRequest.class).getActive());
        exchange.unsubscribeInteractionClass(interaction);
        assertEquals(41, last(Declaration.UnsubInterRequest.class).getInteractionClassHandle());
        for (Message request : wire.requests) assertEnvelope(request);
        assertEquals(14, wire.requests.size());
    }

    @Test
    void explicitEmptyUnpublicationIsNotWholeClassRemoval() throws Exception {
        AttributeSet empty = new AttributeSet();
        exchange.unpublishObjectClassAttributes(objectClass, empty);
        Declaration.UnpubObjAttrsRequest targeted = last(Declaration.UnpubObjAttrsRequest.class);
        com.google.protobuf.Descriptors.FieldDescriptor presence = targeted.getDescriptorForType().findFieldByName("attribute_set_present");
        assertNotNull(presence);
        assertTrue(targeted.hasField(presence));
        assertEquals(true, targeted.getField(presence));
        assertEquals(0, targeted.getAttributeHandlesCount());
        assertEquals(1, wire.requests.size());
        exchange.unpublishObjectClass(objectClass);
        Declaration.UnpubObjAttrsRequest whole = last(Declaration.UnpubObjAttrsRequest.class);
        assertFalse(whole.hasField(presence));
        assertEquals(0, whole.getAttributeHandlesCount());
        exchange.unsubscribeObjectClass(objectClass);
        assertEquals(0, last(Declaration.UnsubObjAttrsRequest.class).getAttributeHandlesCount());
        assertEquals(3, wire.requests.size());
    }

    @Test
    void nullUnpublicationAttributeSetIsNotTreatedAsWholeClass() {
        assertThrows(AttributeNotDefined.class, () -> exchange.unpublishObjectClassAttributes(objectClass, null));
        assertTrue(wire.requests.isEmpty());
    }

    @Test
    void explicitEmptyUnsubscriptionIsNotWholeClassRemoval() throws Exception {
        exchange.unsubscribeObjectClassAttributes(objectClass, new AttributeSet());
        Declaration.UnsubObjAttrsRequest targeted = last(Declaration.UnsubObjAttrsRequest.class);
        com.google.protobuf.Descriptors.FieldDescriptor presence = targeted.getDescriptorForType().findFieldByName("attribute_set_present");
        assertNotNull(presence);
        assertTrue(targeted.hasField(presence));
        assertEquals(true, targeted.getField(presence));
        assertEquals(0, targeted.getAttributeHandlesCount());
        exchange.unsubscribeObjectClass(objectClass);
        Declaration.UnsubObjAttrsRequest whole = last(Declaration.UnsubObjAttrsRequest.class);
        assertFalse(whole.hasField(presence));
        assertEquals(0, whole.getAttributeHandlesCount());
        assertThrows(AttributeNotDefined.class, () -> exchange.unsubscribeObjectClassAttributes(objectClass, null));
        assertEquals(2, wire.requests.size());
    }

    @Test
    void reservationsAndBothRegistrationsUseTheActualAssignedName() throws Exception {
        exchange.reserveObjectInstanceName("car");
        assertEquals("car", last(rti.v1.Object.ReserveObjectInstanceNameRequest.class).getObjectName());
        exchange.releaseObjectInstanceName("car");
        assertEquals("car", last(rti.v1.Object.ReleaseObjectInstanceNameRequest.class).getObjectName());
        exchange.reserveMultipleObjectInstanceName(Set.of("one", "two"));
        assertEquals(Set.of("one", "two"), new HashSet<>(last(rti.v1.Object.ReserveMultipleObjectInstanceNamesRequest.class).getObjectNamesList()));
        exchange.releaseMultipleObjectInstanceName(Set.of("one", "two"));
        assertEquals(Set.of("one", "two"), new HashSet<>(last(rti.v1.Object.ReleaseMultipleObjectInstanceNamesRequest.class).getObjectNamesList()));
        wire.response = rti.v1.Object.RegisterObjectResponse.newBuilder().setObjectHandle(31).setObjectName("HLA-generated").build();
        assertEquals(object, exchange.registerObjectInstance(objectClass));
        assertEquals("", last(rti.v1.Object.RegisterObjectRequest.class).getObjectName());
        assertEquals("HLA-generated", exchange.getObjectInstanceName(object));
        assertEquals(objectClass, exchange.getKnownObjectClassHandle(object));
        wire.response = rti.v1.Object.RegisterObjectResponse.newBuilder().setObjectHandle(32).setObjectName("reserved").build();
        assertEquals(Handles.objectInstance(32), exchange.registerObjectInstance(objectClass, "reserved"));
        assertEquals("reserved", last(rti.v1.Object.RegisterObjectRequest.class).getObjectName());
        assertEquals(Handles.objectInstance(32), exchange.getObjectInstanceHandle("reserved"));
        for (Message request : wire.requests) assertEnvelope(request);
    }

    @Test
    void receiveOrderPayloadsAndRequestTagsAreCopied() throws Exception {
        discover();
        byte[] bytes = {1, 2};
        byte[] tag = {3, 4};
        Attributes attributes = new Attributes();
        attributes.put(Handles.attribute(51), bytes);
        exchange.updateAttributeValues(object, attributes, tag);
        rti.v1.Object.UpdateAttributeValuesRequest update = last(rti.v1.Object.UpdateAttributeValuesRequest.class);
        Parameters parameters = new Parameters();
        parameters.put(Handles.parameter(61), bytes);
        exchange.sendInteraction(interaction, parameters, tag);
        rti.v1.Object.SendInteractionRequest send = last(rti.v1.Object.SendInteractionRequest.class);
        exchange.requestAttributeValueUpdate(object, attributes(), tag);
        rti.v1.Object.RequestAttributeValueUpdateRequest instanceRequest = last(rti.v1.Object.RequestAttributeValueUpdateRequest.class);
        exchange.requestAttributeValueUpdate(objectClass, attributes(), tag);
        rti.v1.Object.RequestClassAttributeValueUpdateRequest classRequest = last(rti.v1.Object.RequestClassAttributeValueUpdateRequest.class);
        bytes[0] = 99;
        tag[0] = 99;
        assertFalse(update.hasLogicalTime());
        assertFalse(send.hasLogicalTime());
        assertEquals(0, update.getMessageRetractionHandle());
        assertEquals(0, send.getMessageRetractionHandle());
        assertEquals(ByteString.copyFrom(new byte[]{1, 2}), update.getAttributesOrThrow(51));
        assertEquals(ByteString.copyFrom(new byte[]{1, 2}), send.getParametersOrThrow(61));
        assertEquals(ByteString.copyFrom(new byte[]{3, 4}), update.getUserSuppliedTag());
        assertEquals(update.getUserSuppliedTag(), send.getUserSuppliedTag());
        assertEquals(update.getUserSuppliedTag(), instanceRequest.getUserSuppliedTag());
        assertEquals(update.getUserSuppliedTag(), classRequest.getUserSuppliedTag());
        assertEquals(List.of(51L), instanceRequest.getAttributeHandlesList());
        assertEquals(List.of(51L), classRequest.getAttributeHandlesList());
        for (Message request : wire.requests) assertEnvelope(request);
    }

    @Test
    void timestampedMethodsShareUniqueProducerScopedRetractionSequence() throws Exception {
        context.regulating = true;
        discover();
        MessageRetractionReturn update = exchange.updateAttributeValues(object, new Attributes(), new byte[]{1}, TimeValues.time(10));
        rti.v1.Object.UpdateAttributeValuesRequest updateRequest = last(rti.v1.Object.UpdateAttributeValuesRequest.class);
        MessageRetractionReturn send = exchange.sendInteraction(interaction, new Parameters(), new byte[]{2}, TimeValues.time(11));
        rti.v1.Object.SendInteractionRequest sendRequest = last(rti.v1.Object.SendInteractionRequest.class);
        MessageRetractionReturn delete = exchange.deleteObjectInstance(object, new byte[]{3}, TimeValues.time(12));
        rti.v1.Object.DeleteObjectInstanceRequest deleteRequest = last(rti.v1.Object.DeleteObjectInstanceRequest.class);
        assertTrue(update.retractionHandleIsValid);
        assertTrue(send.retractionHandleIsValid);
        assertTrue(delete.retractionHandleIsValid);
        assertEquals(Set.of(1L, 2L, 3L), Set.of(Handles.value(update.handle), Handles.value(send.handle), Handles.value(delete.handle)));
        assertEquals(Handles.value(update.handle), updateRequest.getMessageRetractionHandle());
        assertEquals(Handles.value(send.handle), sendRequest.getMessageRetractionHandle());
        assertEquals(Handles.value(delete.handle), deleteRequest.getMessageRetractionHandle());
        assertEquals(10, updateRequest.getLogicalTime());
        assertEquals(11, sendRequest.getLogicalTime());
        assertEquals(12, deleteRequest.getLogicalTime());
        assertEquals(7, Handles.retractionProducer(delete.handle));
        assertEquals(9, Handles.retractionGeneration(delete.handle));
        assertEquals("fed", Handles.retractionFederation(delete.handle));
        exchange.retract(delete.handle);
        assertEquals(3, last(rti.v1.Object.RetractRequest.class).getMessageRetractionHandle());
        assertEquals("car", exchange.getObjectInstanceName(object));
    }

    @Test
    void timestampedNonregulatingSendsNeverClaimValidRetractionHandles() throws Exception {
        discover();
        MessageRetractionReturn update = exchange.updateAttributeValues(object, new Attributes(), new byte[0], TimeValues.time(10));
        assertFalse(update.retractionHandleIsValid);
        assertNull(update.handle);
        assertTrue(last(rti.v1.Object.UpdateAttributeValuesRequest.class).hasLogicalTime());
        assertEquals(0, last(rti.v1.Object.UpdateAttributeValuesRequest.class).getMessageRetractionHandle());
        MessageRetractionReturn send = exchange.sendInteraction(interaction, new Parameters(), new byte[0], TimeValues.time(10));
        assertFalse(send.retractionHandleIsValid);
        assertNull(send.handle);
        assertEquals(0, last(rti.v1.Object.SendInteractionRequest.class).getMessageRetractionHandle());
        MessageRetractionReturn delete = exchange.deleteObjectInstance(object, new byte[0], TimeValues.time(10));
        assertFalse(delete.retractionHandleIsValid);
        assertNull(delete.handle);
        assertEquals(0, last(rti.v1.Object.DeleteObjectInstanceRequest.class).getMessageRetractionHandle());
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
    }

    @Test
    void foreignStaleAndUnissuedRetractionHandlesCannotBorrowLocalWireIds() throws Exception {
        context.regulating = true;
        MessageRetractionReturn sent = exchange.sendInteraction(interaction, new Parameters(), new byte[0], TimeValues.time(10));
        int calls = wire.requests.size();
        assertThrows(InvalidMessageRetractionHandle.class, () -> exchange.retract(Handles.retraction("fed", 9, 8, Handles.value(sent.handle))));
        assertThrows(InvalidMessageRetractionHandle.class, () -> exchange.retract(Handles.retraction("fed", 8, 7, Handles.value(sent.handle))));
        assertThrows(InvalidMessageRetractionHandle.class, () -> exchange.retract(Handles.retraction("fed", 9, 7, 999)));
        assertThrows(InvalidMessageRetractionHandle.class, () -> exchange.retract(Handles.retraction(9, 7, Handles.value(sent.handle))));
        assertEquals(calls, wire.requests.size());
        wire.fail("MessageCanNoLongerBeRetracted");
        assertThrows(MessageCanNoLongerBeRetracted.class, () -> exchange.retract(sent.handle));
    }

    @Test
    void federationNamesSeparateOtherwiseIdenticalRetractionScopes() throws Exception {
        context.regulating = true;
        MessageRetractionReturn local = exchange.sendInteraction(interaction, new Parameters(), new byte[0], TimeValues.time(10));
        Context otherContext = new Context(wire);
        otherContext.regulating = true;
        otherContext.federation = "another-federation";
        ExchangeServices other = new ExchangeServices(otherContext);
        MessageRetractionReturn foreign = other.sendInteraction(interaction, new Parameters(), new byte[0], TimeValues.time(10));
        assertEquals(Handles.value(local.handle), Handles.value(foreign.handle));
        assertEquals(Handles.retractionGeneration(local.handle), Handles.retractionGeneration(foreign.handle));
        assertEquals(Handles.retractionProducer(local.handle), Handles.retractionProducer(foreign.handle));
        int calls = wire.requests.size();
        assertAll(
                () -> assertNotEquals(local.handle, foreign.handle),
                () -> assertThrows(InvalidMessageRetractionHandle.class, () -> exchange.retract(foreign.handle)),
                () -> assertThrows(InvalidMessageRetractionHandle.class, () -> other.retract(local.handle)));
        assertEquals(calls, wire.requests.size());
        exchange.retract(local.handle);
        assertEquals("fed", last(rti.v1.Object.RetractRequest.class).getFederationName());
    }

    @Test
    void grantedRetractionHistoryDoesNotGrowWithEverySendOrLoseTheCanonicalError() throws Exception {
        context.regulating = true;
        int before = retainedServiceCollectionEntries();
        MessageRetractionReturn oldest = null;
        for (int i = 0; i < 2000; i++) {
            MessageRetractionReturn sent = exchange.sendInteraction(interaction, new Parameters(), new byte[0], TimeValues.time(10));
            if (oldest == null) oldest = sent;
        }
        context.logicalTime = 10;
        exchange.advanceKnowledge(10);
        assertEquals(before, retainedServiceCollectionEntries(), "Do not retain one entry per expired TSO send");
        wire.fail("MessageCanNoLongerBeRetracted");
        MessageRetractionHandle expired = oldest.handle;
        int calls = wire.requests.size();
        assertThrows(MessageCanNoLongerBeRetracted.class, () -> exchange.retract(expired));
        assertEquals(calls + 1, wire.requests.size(), "The backend must adjudicate expiration");
    }

    @Test
    void failedSendGapsWithinCounterBoundsAreValidatedByTheBackend() throws Exception {
        context.regulating = true;
        wire.fail("InteractionClassNotPublished");
        assertThrows(InteractionClassNotPublished.class, () -> exchange.sendInteraction(interaction, new Parameters(), new byte[0], TimeValues.time(10)));
        wire.clearFailure();
        MessageRetractionReturn sent = exchange.sendInteraction(interaction, new Parameters(), new byte[0], TimeValues.time(11));
        assertEquals(2, Handles.value(sent.handle));
        wire.fail("InvalidMessageRetractionHandle");
        int calls = wire.requests.size();
        assertThrows(InvalidMessageRetractionHandle.class, () -> exchange.retract(Handles.retraction("fed", 9, 7, 1)));
        assertEquals(calls + 1, wire.requests.size(), "Counter gaps are authoritative backend state");
        wire.fail("MessageCanNoLongerBeRetracted");
        assertThrows(MessageCanNoLongerBeRetracted.class, () -> exchange.retract(sent.handle));
    }

    @Test
    void clearingKnowledgeInvalidatesEarlierCountersWithoutRecyclingThem() throws Exception {
        context.regulating = true;
        MessageRetractionReturn before = exchange.sendInteraction(interaction, new Parameters(), new byte[0], TimeValues.time(10));
        exchange.clearKnowledge();
        MessageRetractionReturn after = exchange.sendInteraction(interaction, new Parameters(), new byte[0], TimeValues.time(11));
        assertEquals(Handles.value(before.handle) + 1, Handles.value(after.handle));
        int calls = wire.requests.size();
        assertThrows(InvalidMessageRetractionHandle.class, () -> exchange.retract(before.handle));
        assertThrows(InvalidMessageRetractionHandle.class, () -> exchange.retract(Handles.retraction("fed", 9, 7, 0)));
        assertEquals(calls, wire.requests.size());
        exchange.retract(after.handle);
        assertEquals(Handles.value(after.handle), last(rti.v1.Object.RetractRequest.class).getMessageRetractionHandle());
    }

    @Test
    void supportCannotExposeUndiscoveredObjectsAndDiscoveryIsReversible() throws Exception {
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getKnownObjectClassHandle(object));
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceHandle("car"));
        assertTrue(wire.requests.isEmpty());
        discover();
        assertEquals(object, exchange.getObjectInstanceHandle("car"));
        exchange.removed(31, false, 8, 1, 20);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
        exchange.retracted(7, 1);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
        exchange.retracted(8, 1);
        assertEquals("car", exchange.getObjectInstanceName(object));
        assertEquals(objectClass, exchange.getKnownObjectClassHandle(object));
        exchange.removed(31, false, 0, 0, -1);
        exchange.retracted(8, 1);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
    }

    @Test
    void grantsAndRediscoveryExpireSnapshotsButConstraintDisableAllowsRetraction() throws Exception {
        discover();
        exchange.removed(31, true, 8, 1, 20);
        exchange.advanceKnowledge(19);
        exchange.retracted(8, 1);
        assertEquals("car", exchange.getObjectInstanceName(object));
        exchange.removed(31, true, 8, 2, 20);
        exchange.advanceKnowledge(20);
        exchange.retracted(8, 2);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
        discover();
        exchange.removed(31, true, 8, 3, 30);
        exchange.constrainDisabled();
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
        context.logicalTime = 100;
        exchange.advanceKnowledge(100);
        exchange.retracted(8, 3);
        assertEquals("car", exchange.getObjectInstanceName(object));
        context.logicalTime = 20;
        discover();
        exchange.removed(31, true, 8, 4, 30);
        exchange.discovered(31, 22, "new-car");
        exchange.retracted(8, 4);
        assertEquals("new-car", exchange.getObjectInstanceName(object));
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceHandle("car"));
        exchange.removed(31, true, 8, 5, 30);
        exchange.clearKnowledge();
        exchange.retracted(8, 5);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
    }

    @Test
    void receivedTimestampRemovalStaysVisibleUntilItsGrantBoundary() throws Exception {
        discover();
        exchange.removed(31, true, 8, 1, 20);
        assertEquals(object, exchange.getObjectInstanceHandle("car"));
        assertEquals("car", exchange.getObjectInstanceName(object));
        assertEquals(objectClass, exchange.getKnownObjectClassHandle(object));
        exchange.advanceKnowledge(19);
        assertEquals("car", exchange.getObjectInstanceName(object));
        exchange.advanceKnowledge(20);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceHandle("car"));
        exchange.retracted(8, 1);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getKnownObjectClassHandle(object));
    }

    @Test
    void retractionOfVisibleProvisionalRemovalCancelsItsPendingExpiry() throws Exception {
        discover();
        exchange.removed(31, true, 8, 1, 20);
        assertEquals("car", exchange.getObjectInstanceName(object));
        exchange.retracted(8, 1);
        exchange.advanceKnowledge(20);
        assertEquals("car", exchange.getObjectInstanceName(object));
        exchange.constrainDisabled();
        assertEquals(objectClass, exchange.getKnownObjectClassHandle(object));
    }

    @Test
    void timestampRemovalWithoutRetractionIdentityStillHasProvisionalVisibility() throws Exception {
        discover();
        exchange.removed(31, true, 8, 0, 20);
        assertEquals("car", exchange.getObjectInstanceName(object));
        exchange.retracted(8, 0);
        exchange.advanceKnowledge(20);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
        discover();
        exchange.removed(31, true, 8, 0, 30);
        assertEquals("car", exchange.getObjectInstanceName(object));
        exchange.constrainDisabled();
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
        exchange.retracted(8, 0);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
    }

    @Test
    void timestampedReceiveOrderSnapshotSurvivesRecipientTimeAndConstraintChanges() throws Exception {
        discover();
        context.logicalTime = 100;
        exchange.removed(31, false, 8, 1, 20);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
        exchange.advanceKnowledge(200);
        exchange.constrainDisabled();
        exchange.retracted(8, 1);
        assertEquals("car", exchange.getObjectInstanceName(object));
        assertEquals(object, exchange.getObjectInstanceHandle("car"));
        exchange.removed(31, false, 8, 0, 20);
        exchange.retracted(8, 0);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
        discover();
        exchange.removed(31, false, 8, 2, -1);
        exchange.retracted(8, 2);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
    }

    @Test
    void originatedTimestampDeleteForgetsImmediatelyAndExpiresOnlyAtSenderGrant() throws Exception {
        context.regulating = true;
        discover();
        MessageRetractionReturn first = exchange.deleteObjectInstance(object, new byte[0], TimeValues.time(20));
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
        exchange.constrainDisabled();
        exchange.retract(first.handle);
        assertEquals("car", exchange.getObjectInstanceName(object));
        MessageRetractionReturn second = exchange.deleteObjectInstance(object, new byte[0], TimeValues.time(30));
        exchange.advanceKnowledge(30);
        wire.fail("MessageCanNoLongerBeRetracted");
        assertThrows(MessageCanNoLongerBeRetracted.class, () -> exchange.retract(second.handle));
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
    }

    @Test
    void failedDeletesPreserveKnowledgeAndSuccessfulDeletesForgetIt() throws Exception {
        discover();
        wire.fail("DeletePrivilegeNotHeld");
        assertThrows(DeletePrivilegeNotHeld.class, () -> exchange.deleteObjectInstance(object, new byte[0]));
        assertEquals("car", exchange.getObjectInstanceName(object));
        wire.clearFailure();
        byte[] tag = {4};
        exchange.deleteObjectInstance(object, tag);
        tag[0] = 99;
        assertEquals(ByteString.copyFrom(new byte[]{4}), last(rti.v1.Object.DeleteObjectInstanceRequest.class).getUserSuppliedTag());
        assertFalse(last(rti.v1.Object.DeleteObjectInstanceRequest.class).hasLogicalTime());
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
        discover();
        exchange.localDeleteObjectInstance(object);
        assertEquals(31, last(rti.v1.Object.LocalDeleteObjectInstanceRequest.class).getObjectHandle());
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
    }

    @Test
    void allBasicFomOrderAndTransportationLookupsUseRpc() throws Exception {
        wire.response = Support.GetObjectClassHandleResponse.newBuilder().setClassHandle(21).build();
        assertEquals(objectClass, exchange.getObjectClassHandle("Vehicle"));
        assertEquals("Vehicle", last(Support.GetObjectClassHandleRequest.class).getClassName());
        wire.response = Support.GetObjectClassNameResponse.newBuilder().setClassName("Vehicle").build();
        assertEquals("Vehicle", exchange.getObjectClassName(objectClass));
        wire.response = Support.GetAttributeHandleResponse.newBuilder().setAttributeHandle(51).build();
        assertEquals(Handles.attribute(51), exchange.getAttributeHandle(objectClass, "Position"));
        assertEquals(21, last(Support.GetAttributeHandleRequest.class).getClassHandle());
        wire.response = Support.GetAttributeNameResponse.newBuilder().setAttributeName("Position").build();
        assertEquals("Position", exchange.getAttributeName(objectClass, Handles.attribute(51)));
        wire.response = Support.GetInteractionClassHandleResponse.newBuilder().setClassHandle(41).build();
        assertEquals(interaction, exchange.getInteractionClassHandle("Ping"));
        wire.response = Support.GetInteractionClassNameResponse.newBuilder().setClassName("Ping").build();
        assertEquals("Ping", exchange.getInteractionClassName(interaction));
        wire.response = Support.GetParameterHandleResponse.newBuilder().setParameterHandle(61).build();
        assertEquals(Handles.parameter(61), exchange.getParameterHandle(interaction, "Payload"));
        wire.response = Support.GetParameterNameResponse.newBuilder().setParameterName("Payload").build();
        assertEquals("Payload", exchange.getParameterName(interaction, Handles.parameter(61)));
        wire.response = Support.GetOrderTypeResponse.newBuilder().setOrderType(2).build();
        assertEquals(OrderType.TIMESTAMP, exchange.getOrderType("TimeStamp"));
        wire.response = Support.GetOrderNameResponse.newBuilder().setOrderName("Receive").build();
        assertEquals("Receive", exchange.getOrderName(OrderType.RECEIVE));
        assertEquals(1, last(Support.GetOrderNameRequest.class).getOrderType());
        wire.response = Support.GetTransportationTypeResponse.newBuilder().setTransportationType(2).build();
        assertEquals(Handles.transportation(2), exchange.getTransportationTypeHandle("HLAbestEffort"));
        wire.response = Support.GetTransportationNameResponse.newBuilder().setTransportationName("HLAreliable").build();
        assertEquals("HLAreliable", exchange.getTransportationTypeName(Handles.transportation(1)));
        assertEquals(1, last(Support.GetTransportationNameRequest.class).getTransportationType());
        assertEquals(12, wire.requests.size());
    }

    @Test
    void canonicalTrailersAreNotFlattenedAndMembershipPrecedesChannelAccess() throws Exception {
        wire.fail("InvalidUpdateRateDesignator");
        assertThrows(InvalidUpdateRateDesignator.class, () -> exchange.subscribeObjectClassAttributes(objectClass, attributes(), "missing"));
        wire.fail("NameNotFound");
        assertThrows(NameNotFound.class, () -> exchange.getObjectClassHandle("missing"));
        context.member = false;
        int count = wire.requests.size();
        assertThrows(FederateNotExecutionMember.class, () -> exchange.getOrderType("Receive"));
        assertThrows(FederateNotExecutionMember.class, () -> exchange.sendInteraction(interaction, new Parameters(), new byte[0]));
        assertThrows(FederateNotExecutionMember.class, () -> exchange.getObjectInstanceName(object));
        assertEquals(count, wire.requests.size());
    }

    @Test
    void federateLookupsUseAuthenticatedMemberListAndStandardMissingErrors() throws Exception {
        Metadata.Key<byte[]> token = Metadata.Key.of("gorti-session-token-bin", Metadata.BINARY_BYTE_MARSHALLER);
        Metadata headers = new Metadata();
        headers.put(token, new byte[]{1, 2, 3});
        Context authenticated = new Context(ClientInterceptors.intercept(wire, MetadataUtils.newAttachHeadersInterceptor(headers)));
        ExchangeServices services = new ExchangeServices(authenticated);
        wire.response = Federation.ListFederationMembersResponse.newBuilder()
                .addMembers(Federation.FederationMember.newBuilder().setFederateHandle(7).setFederateName("self"))
                .addMembers(Federation.FederationMember.newBuilder().setFederateHandle(8).setFederateName("peer")).build();
        assertEquals(Handles.federate(8), services.getFederateHandle("peer"));
        assertEquals("peer", services.getFederateName(Handles.federate(8)));
        assertArrayEquals(new byte[]{1, 2, 3}, wire.headers.get(token));
        assertEquals("fed", last(Federation.ListFederationMembersRequest.class).getFederationName());
        assertThrows(NameNotFound.class, () -> services.getFederateHandle("missing"));
        assertThrows(InvalidFederateHandle.class, () -> services.getFederateName(Handles.federate(99)));
        wire.fail("FederateNotExecutionMember");
        assertThrows(FederateNotExecutionMember.class, () -> services.getFederateHandle("peer"));
    }

    @Test
    void alreadyGrantedRemovalCannotBeResurrected() {
        discover();
        context.logicalTime = 20;
        exchange.removed(31, true, 8, 1, 20);
        exchange.retracted(8, 1);
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(object));
    }

    @Test
    void nullTimestampIsNotSilentlyDowngradedToReceiveOrder() {
        discover();
        assertThrows(InvalidLogicalTime.class, () -> exchange.updateAttributeValues(object, new Attributes(), new byte[0], null));
        assertThrows(InvalidLogicalTime.class, () -> exchange.sendInteraction(interaction, new Parameters(), new byte[0], null));
        assertThrows(InvalidLogicalTime.class, () -> exchange.deleteObjectInstance(object, new byte[0], null));
        assertTrue(wire.requests.isEmpty());
    }

    private void discover() { exchange.discovered(31, 21, "car"); }

    private int retainedServiceCollectionEntries() throws IllegalAccessException {
        int entries = 0;
        for (Field field : ExchangeServices.class.getDeclaredFields()) {
            field.setAccessible(true);
            Object value = field.get(exchange);
            if (value instanceof Collection<?>) entries += ((Collection<?>) value).size();
            if (value instanceof Map<?, ?>) entries += ((Map<?, ?>) value).size();
        }
        return entries;
    }

    private <T> T last(Class<T> type) { return type.cast(wire.requests.get(wire.requests.size() - 1)); }

    private void assertSubscription(boolean active, String rate) {
        Declaration.SubObjAttrsRequest request = last(Declaration.SubObjAttrsRequest.class);
        assertTrue(request.hasActive());
        assertEquals(active, request.getActive());
        assertEquals(rate, request.getUpdateRateDesignator());
        assertEquals(List.of(51L), request.getAttributeHandlesList());
    }

    private static void assertEnvelope(Message message) {
        assertEquals(Common.WireVersion.WIRE_VERSION_V1.getValueDescriptor(), message.getField(message.getDescriptorForType().findFieldByName("wire_version")));
        assertEquals("fed", message.getField(message.getDescriptorForType().findFieldByName("federation_name")));
        assertEquals(7L, message.getField(message.getDescriptorForType().findFieldByName("federate_handle")));
    }

    private static AttributeSet attributes() {
        AttributeSet values = new AttributeSet();
        values.add(Handles.attribute(51));
        return values;
    }

    private static final class AttributeSet extends HashSet<AttributeHandle> implements AttributeHandleSet {
        @Override public AttributeSet clone() { return (AttributeSet) super.clone(); }
    }

    private static final class Attributes extends HashMap<AttributeHandle, byte[]> implements AttributeHandleValueMap {
        public ByteWrapper getValueReference(AttributeHandle key) { return new ByteWrapper(get(key)); }
        public ByteWrapper getValueReference(AttributeHandle key, ByteWrapper target) { throw new UnsupportedOperationException(); }
    }

    private static final class Parameters extends HashMap<ParameterHandle, byte[]> implements ParameterHandleValueMap {
        public ByteWrapper getValueReference(ParameterHandle key) { return new ByteWrapper(get(key)); }
        public ByteWrapper getValueReference(ParameterHandle key, ByteWrapper target) { throw new UnsupportedOperationException(); }
    }

    private static final class Context implements ServiceContext {
        final Channel wire;
        boolean member = true;
        boolean regulating;
        double logicalTime;
        String federation = "fed";
        Context(Channel wire) { this.wire = wire; }
        public void requireMember() throws RTIexception { if (!member) throw new FederateNotExecutionMember("not joined"); }
        public Channel channel() { assertTrue(member, "channel accessed before membership check"); return wire; }
        public String federation() { return federation; }
        public long federate() { return 7; }
        public long generation() { return 9; }
        public boolean regulating() { return regulating; }
        public double logicalTime() { return logicalTime; }
    }

    private static final class RecordingChannel extends Channel {
        final List<Message> requests = new ArrayList<>();
        Message response = Common.Empty.getDefaultInstance();
        Status failure;
        Metadata trailers;
        Metadata headers;

        void fail(String exception) {
            failure = Status.FAILED_PRECONDITION.withDescription("canonical server rejection");
            trailers = new Metadata();
            trailers.put(Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER), exception);
        }

        void clearFailure() { failure = null; }
        public String authority() { return "exchange-test"; }

        public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method, CallOptions options) {
            return new ClientCall<ReqT, RespT>() {
                private Listener<RespT> listener;
                public void start(Listener<RespT> target, Metadata metadata) { listener = target; headers = metadata; }
                public void request(int count) { }
                public void cancel(String message, Throwable cause) { }
                public void sendMessage(ReqT request) { requests.add((Message) request); }
                @SuppressWarnings("unchecked")
                public void halfClose() {
                    if (failure != null) { listener.onClose(failure, trailers); return; }
                    listener.onMessage((RespT) response);
                    listener.onClose(Status.OK, new Metadata());
                }
            };
        }
    }
}
