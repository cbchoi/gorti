package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import com.google.protobuf.Message;
import hla.rti1516e.*;
import hla.rti1516e.time.HLAinteger64Time;
import hla.rti1516e.exceptions.*;
import io.grpc.*;
import java.io.*;
import java.util.*;
import org.gorti.rti.types.Handles;
import org.gorti.rti.types.Integer64TimeFactory;
import org.gorti.rti.types.TimeValues;
import org.junit.jupiter.api.Test;
import rti.v1.Common;
import rti.v1.Stream;
import rti.v1.Time;

class ExactTimeWireTest {
    private final Integer64TimeFactory factory = new Integer64TimeFactory();

    @Test void integerBoundariesNeverPopulateLegacyDoubles() {
        for (long value : new long[]{0, (1L << 53) + 1, Long.MAX_VALUE}) {
            LogicalTime time = factory.makeTime(value);
            Time.TARRequest request = TimeWire.write(Time.TARRequest.newBuilder(), "logical_time", time, TimeWire.INTEGER).build();
            assertEquals(value, request.getLogicalTimeValue().getInteger64Value());
            assertEquals(0, request.getLogicalTime());
            assertEquals(value, ((HLAinteger64Time) TimeWire.read(request, "logical_time", TimeWire.INTEGER)).getValue());
            rti.v1.Object.SendInteractionRequest send = TimeWire.write(rti.v1.Object.SendInteractionRequest.newBuilder(), "logical_time", time, TimeWire.INTEGER).build();
            assertFalse(send.hasLogicalTime());
        }
    }

    @Test void mixedRepresentationsAndPresentLegacyZeroFail() {
        assertThrows(IllegalArgumentException.class, () -> TimeWire.write(Time.TARRequest.newBuilder(), "logical_time", TimeValues.time(1), TimeWire.INTEGER));
        assertThrows(IllegalArgumentException.class, () -> TimeWire.write(Time.TARRequest.newBuilder(), "logical_time", factory.makeTime(1), TimeWire.FLOAT));
        rti.v1.Object.SendInteractionRequest mixed = rti.v1.Object.SendInteractionRequest.newBuilder().setLogicalTime(0)
                .setLogicalTimeValue(Common.LogicalTimeValue.newBuilder().setInteger64Value(1)).build();
        assertThrows(IllegalArgumentException.class, () -> TimeWire.read(mixed, "logical_time", TimeWire.INTEGER));
        Time.TARRequest negativeZero = Time.TARRequest.newBuilder().setLogicalTime(-0.0)
                .setLogicalTimeValue(Common.LogicalTimeValue.newBuilder().setInteger64Value(1)).build();
        assertThrows(IllegalArgumentException.class, () -> TimeWire.read(negativeZero, "logical_time", TimeWire.INTEGER));
    }

    @Test void objectServicesValidateBeforeRpcAndKeepExactTime() throws Exception {
        RecordingChannel wire = new RecordingChannel();
        ServiceContext context = integerContext(wire);
        ExchangeServices exchange = new ExchangeServices(context);
        exchange.discovered(31, 21, "car");
        ParameterHandleValueMap parameters = org.gorti.rti.types.HandleCollections.parameterValueMapFactory().create(0);
        AttributeHandleValueMap attributes = org.gorti.rti.types.HandleCollections.attributeValueMapFactory().create(0);
        long value = (1L << 53) + 1;
        exchange.sendInteraction(Handles.interactionClass(41), parameters, new byte[0], factory.makeTime(value));
        rti.v1.Object.SendInteractionRequest send = (rti.v1.Object.SendInteractionRequest) wire.requests.get(0);
        assertEquals(value, send.getLogicalTimeValue().getInteger64Value()); assertFalse(send.hasLogicalTime());
        exchange.updateAttributeValues(Handles.objectInstance(31), attributes, new byte[0], factory.makeTime(value));
        assertEquals(value, ((rti.v1.Object.UpdateAttributeValuesRequest) wire.requests.get(1)).getLogicalTimeValue().getInteger64Value());
        exchange.deleteObjectInstance(Handles.objectInstance(31), new byte[0], factory.makeTime(value));
        assertEquals(value, ((rti.v1.Object.DeleteObjectInstanceRequest) wire.requests.get(2)).getLogicalTimeValue().getInteger64Value());
        assertThrows(InvalidLogicalTime.class, () -> exchange.sendInteraction(Handles.interactionClass(41), parameters, new byte[0], TimeValues.time(1)));
        assertEquals(3, wire.requests.size());
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(Handles.objectInstance(31)));
    }

    @Test void callbacksAndCheckpointsPreserveInteger64() throws Exception {
        long value = (1L << 53) + 1;
        List<Long> received = new ArrayList<>();
        Stream.FederateEvent event = Stream.FederateEvent.newBuilder().setSaveInitiate(Stream.InitiateFederateSave.newBuilder()
                .setLabel("save").setSaveTimeValue(Common.LogicalTimeValue.newBuilder().setInteger64Value(value))).build();
        SaveRestoreCallbacks.deliver(event, new NullFederateAmbassador() {
            @Override public void initiateFederateSave(String label, LogicalTime time) { received.add(((HLAinteger64Time)time).getValue()); }
        }, TimeWire.INTEGER);
        assertEquals(List.of(value), received);
        ExchangeKnowledge knowledge = new ExchangeKnowledge(); knowledge.discovered(1, 2, "saved");
        knowledge.removed(1, true, 8, 1, factory.makeTime(value), factory.makeTime(0));
        DdmServices.State regions = DdmServices.State.readFrom(new DataInputStream(new ByteArrayInputStream(new byte[4])));
        RuntimeCheckpoint source = new RuntimeCheckpoint(new ExchangeServices.State(knowledge, 0, 0), regions, Set.of(),
                true, true, false, false, false, factory.makeTime(value));
        RuntimeCheckpoint saved = RuntimeCheckpoint.decode(source.encode("fed", 9, 7, "save"), "fed", 9, 7, "save");
        assertEquals(value, ((HLAinteger64Time)saved.logicalTimeValue).getValue());
        ExchangeServices restored = new ExchangeServices(integerContext(new RecordingChannel())); restored.restore(saved.exchangeState, 0);
        restored.advanceKnowledge(factory.makeTime(value - 1));
        assertEquals("saved", restored.getObjectInstanceName(Handles.objectInstance(1)));
        restored.advanceKnowledge(factory.makeTime(value));
        assertThrows(ObjectInstanceNotKnown.class, () -> restored.getObjectInstanceName(Handles.objectInstance(1)));
    }

    private ServiceContext integerContext(Channel wire) {
        return new ServiceContext() {
            public void requireMember() { }
            public Channel channel() { return wire; }
            public String federation() { return "fed"; }
            public long federate() { return 7; }
            public long generation() { return 9; }
            public boolean regulating() { return true; }
            public double logicalTime() { throw new AssertionError("Integer64 converted to double"); }
            public LogicalTime logicalTimeValue() { return factory.makeTime(0); }
            public String timeImplementation() { return TimeWire.INTEGER; }
        };
    }

    static final class RecordingChannel extends Channel {
        final List<Message> requests = new ArrayList<>();
        Message response = Common.Empty.getDefaultInstance();
        Status failure;
        public String authority() { return "exact-wire-test"; }
        public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method, CallOptions options) {
            return new ClientCall<ReqT, RespT>() {
                private Listener<RespT> listener;
                public void start(Listener<RespT> target, Metadata headers) { listener = target; }
                public void request(int count) { }
                public void cancel(String message, Throwable cause) { }
                public void sendMessage(ReqT request) { requests.add((Message) request); }
                @SuppressWarnings("unchecked") public void halfClose() {
                    if (failure != null) { listener.onClose(failure, new Metadata()); return; }
                    listener.onMessage((RespT) response); listener.onClose(Status.OK, new Metadata());
                }
            };
        }
    }
}
