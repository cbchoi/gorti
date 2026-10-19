package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.FederateAmbassador;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.SynchronizationPointFailureReason;
import hla.rti1516e.exceptions.RTIinternalError;
import java.lang.reflect.Array;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.List;
import java.util.Set;
import org.junit.jupiter.api.Test;
import rti.v1.Stream;

class JavaAnnexApplicable20261003Test {
    @Test void nullAmbassadorInvokesEveryCallbackAsNoOp() throws Exception {
        NullFederateAmbassador target = new NullFederateAmbassador();
        Method[] callbacks = FederateAmbassador.class.getDeclaredMethods();
        assertEquals(60, callbacks.length);
        for (Method callback : callbacks) {
            Method implementation = NullFederateAmbassador.class.getDeclaredMethod(
                    callback.getName(), callback.getParameterTypes());
            assertEquals(void.class, implementation.getReturnType());
            Object[] arguments = new Object[callback.getParameterCount()];
            Class<?>[] types = callback.getParameterTypes();
            for (int i = 0; i < types.length; i++) {
                if (types[i].isPrimitive()) arguments[i] = Array.get(Array.newInstance(types[i], 1), 0);
            }
            assertNull(implementation.invoke(target, arguments), callback.toString());
        }
    }

    @Test void synchronizationRegistrationCallbacksPreserveLabelAndBothFailureReasons() throws Exception {
        List<String> calls = new ArrayList<>();
        List<SynchronizationPointFailureReason> reasons = new ArrayList<>();
        NullFederateAmbassador target = new NullFederateAmbassador() {
            @Override public void synchronizationPointRegistrationSucceeded(String label) {
                calls.add("success:" + label);
            }
            @Override public void synchronizationPointRegistrationFailed(
                    String label, SynchronizationPointFailureReason reason) {
                calls.add("failure:" + label);
                reasons.add(reason);
            }
        };
        assertTrue(deliver(Stream.FederateEvent.newBuilder().setSyncRegistrationSucceeded(
                Stream.SyncRegistrationSucceeded.newBuilder().setLabel("accepted")).build(), target));
        assertTrue(deliver(Stream.FederateEvent.newBuilder().setSyncRegistrationFailed(
                Stream.SyncRegistrationFailed.newBuilder().setLabel("duplicate").setReason(
                        Stream.SyncPointFailureReason.SYNC_POINT_FAILURE_REASON_LABEL_NOT_UNIQUE)).build(), target));
        assertTrue(deliver(Stream.FederateEvent.newBuilder().setSyncRegistrationFailed(
                Stream.SyncRegistrationFailed.newBuilder().setLabel("nonmember").setReason(
                        Stream.SyncPointFailureReason.SYNC_POINT_FAILURE_REASON_SET_MEMBER_NOT_JOINED)).build(), target));
        assertEquals(List.of("success:accepted", "failure:duplicate", "failure:nonmember"), calls);
        assertEquals(List.of(SynchronizationPointFailureReason.SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE,
                SynchronizationPointFailureReason.SYNCHRONIZATION_SET_MEMBER_NOT_JOINED), reasons);
        assertThrows(RTIinternalError.class, () -> deliver(Stream.FederateEvent.newBuilder()
                .setSyncRegistrationFailed(Stream.SyncRegistrationFailed.newBuilder()
                        .setLabel("unknown").setReasonValue(99)).build(), target));
        assertEquals(3, calls.size(), "An invalid reason must not invoke a user callback");
    }

    @Test void singleAndMultipleReservationCallbacksPreserveRequestedNames() throws Exception {
        List<String> calls = new ArrayList<>();
        NullFederateAmbassador target = new NullFederateAmbassador() {
            @Override public void objectInstanceNameReservationSucceeded(String name) {
                assertEquals("accepted", name); calls.add("single-success");
            }
            @Override public void objectInstanceNameReservationFailed(String name) {
                assertEquals("taken", name); calls.add("single-failure");
            }
            @Override public void multipleObjectInstanceNameReservationSucceeded(Set<String> names) {
                assertEquals(Set.of("a", "b"), names); names.clear(); calls.add("multiple-success");
            }
            @Override public void multipleObjectInstanceNameReservationFailed(Set<String> names) {
                assertEquals(Set.of("a", "b"), names); names.clear(); calls.add("multiple-failure");
            }
        };
        Stream.FederateEvent multipleSuccess = Stream.FederateEvent.newBuilder().setReservationMultiSucceeded(
                Stream.MultipleObjectInstanceNameReservationSucceeded.newBuilder()
                        .addObjectNames("a").addObjectNames("b")).build();
        Stream.FederateEvent multipleFailure = Stream.FederateEvent.newBuilder().setReservationMultiFailed(
                Stream.MultipleObjectInstanceNameReservationFailed.newBuilder()
                        .addRequestedNames("a").addRequestedNames("b").addCollidingNames("a")).build();
        assertTrue(deliver(Stream.FederateEvent.newBuilder().setReservationSucceeded(
                Stream.ObjectInstanceNameReservationSucceeded.newBuilder().setObjectName("accepted")).build(), target));
        assertTrue(deliver(Stream.FederateEvent.newBuilder().setReservationFailed(
                Stream.ObjectInstanceNameReservationFailed.newBuilder().setObjectName("taken")).build(), target));
        assertTrue(deliver(multipleSuccess, target));
        assertTrue(deliver(multipleFailure, target));
        assertEquals(List.of("single-success", "single-failure", "multiple-success", "multiple-failure"), calls);
        assertEquals(List.of("a", "b"), multipleSuccess.getReservationMultiSucceeded().getObjectNamesList());
        assertEquals(List.of("a", "b"), multipleFailure.getReservationMultiFailed().getRequestedNamesList());
        assertEquals(List.of("a"), multipleFailure.getReservationMultiFailed().getCollidingNamesList());
    }

    private static boolean deliver(Stream.FederateEvent event, FederateAmbassador target) throws Exception {
        return ExchangeCallbacks.deliver(event, target, "annex-test", 1, () -> { });
    }
}
