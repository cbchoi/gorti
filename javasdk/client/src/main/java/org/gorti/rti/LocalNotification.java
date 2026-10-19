package org.gorti.rti;

import com.google.gson.*;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import java.io.IOException;
import java.math.BigInteger;
import java.security.SecureRandom;
import java.util.*;
import org.gorti.rti.types.HandleCollections;
import org.gorti.rti.types.Handles;
import rti.v1.Common;
import rti.v1.Savepoint;

/** Immutable result data, never an RPC closure or an authorization capability. */
final class LocalNotification implements CallbackSink.Invocation {
    private static final SecureRandom RANDOM = new SecureRandom();
    final String localId, scope, kind;
    final long order;
    private final JsonObject payload;

    private LocalNotification(String id, long order, String scope, String kind, JsonObject payload) {
        this.localId = id; this.order = order; this.scope = scope; this.kind = kind; this.payload = payload.deepCopy();
    }
    private static LocalNotification create(String kind, JsonObject payload) throws RTIinternalError {
        byte[] bytes = new byte[32]; RANDOM.nextBytes(bytes);
        StringBuilder id = new StringBuilder(64);
        for (byte value : bytes) id.append(String.format(Locale.ROOT, "%02x", value & 255));
        LocalNotification result = new LocalNotification(id.toString(), 0, kind.equals("reportFederationExecutions") ? "connection" : "member", kind, payload);
        try { result.validatePayload(-1); } catch (IOException | RuntimeException invalid) { throw new RTIinternalError("Invalid local callback result", invalid); }
        return result;
    }
    LocalNotification ordered(long value) { return new LocalNotification(localId, value, scope, kind, payload); }

    static LocalNotification ownership(String kind, long object, long attribute, long owner) throws RTIinternalError {
        JsonObject payload = new JsonObject(); number(payload, "object_handle", object); number(payload, "attribute_handle", attribute);
        if (kind.equals("informAttributeOwnership")) number(payload, "owner_federate", owner);
        return create(kind, payload);
    }
    static LocalNotification cancelled(long object, List<Long> attributes) throws RTIinternalError {
        JsonObject payload = new JsonObject(); number(payload, "object_handle", object);
        JsonArray values = new JsonArray(); for (long attribute : attributes) values.add(new BigInteger(Long.toUnsignedString(attribute)));
        payload.add("attribute_handles", values); return create("confirmAttributeOwnershipAcquisitionCancellation", payload);
    }
    static LocalNotification transport(String kind, long object, long attribute, long federate, long interaction, Common.TransportationType type) throws RTIinternalError {
        JsonObject payload = new JsonObject();
        if (kind.equals("reportAttributeTransportationType")) { number(payload, "object_handle", object); number(payload, "attribute_handle", attribute); }
        else { number(payload, "federate_handle", federate); number(payload, "interaction_class_handle", interaction); }
        if (type != Common.TransportationType.TRANSPORTATION_TYPE_RELIABLE && type != Common.TransportationType.TRANSPORTATION_TYPE_BEST_EFFORT)
            throw new RTIinternalError("Invalid local transportation result");
        payload.addProperty("transport_type", type == Common.TransportationType.TRANSPORTATION_TYPE_RELIABLE ? 1 : 2);
        return create(kind, payload);
    }
    static LocalNotification saveStatus(Savepoint.QuerySaveStateResponse response) throws RTIinternalError {
        JsonObject payload = new JsonObject(); JsonArray statuses = new JsonArray();
        for (Savepoint.FederateSaveStatus status : response.getFederateStatusesList()) {
            JsonObject row = new JsonObject(); number(row, "federate_handle", status.getFederateHandle()); row.addProperty("status", status.getStatusValue()); statuses.add(row);
        }
        payload.add("statuses", statuses); return create("federationSaveStatusResponse", payload);
    }
    static LocalNotification restoreStatus(Savepoint.QueryRestoreStateResponse response) throws RTIinternalError {
        JsonObject payload = new JsonObject(); JsonArray statuses = new JsonArray();
        for (Savepoint.FederateRestoreStatus status : response.getFederateStatusesList()) {
            JsonObject row = new JsonObject(); number(row, "pre_restore_handle", status.getPreRestoreHandle());
            number(row, "post_restore_handle", status.getPostRestoreHandle()); row.addProperty("status", status.getStatusValue()); statuses.add(row);
        }
        payload.add("statuses", statuses); return create("federationRestoreStatusResponse", payload);
    }
    static LocalNotification executions(Collection<FederationExecutionInformation> values) throws RTIinternalError {
        JsonObject payload = new JsonObject(); JsonArray executions = new JsonArray();
        for (FederationExecutionInformation value : values) {
            JsonObject row = new JsonObject(); row.addProperty("name", value.federationExecutionName);
            row.addProperty("logical_time_implementation_name", value.logicalTimeImplementationName); executions.add(row);
        }
        payload.add("executions", executions); return create("reportFederationExecutions", payload);
    }

    JsonObject encode(long generation) throws IOException {
        if (!localId.matches("[0-9a-f]{64}") || order == 0) throw new IOException("Invalid local callback identity/order");
        validatePayload(generation);
        JsonObject result = new JsonObject(); result.addProperty("local_id", localId); number(result, "order", order);
        result.addProperty("scope", scope); result.addProperty("kind", kind); result.add("payload", payload.deepCopy()); return result;
    }
    static LocalNotification decode(JsonObject row, long generation) throws IOException {
        try {
            RuntimeJson.fields(row, "local_id", "order", "scope", "kind", "payload");
            if (!row.get("payload").isJsonObject()) throw new IOException("Local callback payload must be an object");
            LocalNotification result = new LocalNotification(RuntimeJson.string(row, "local_id"), RuntimeJson.unsigned(row, "order"),
                    RuntimeJson.string(row, "scope"), RuntimeJson.string(row, "kind"), row.getAsJsonObject("payload"));
            result.encode(generation); return result;
        } catch (RuntimeException invalid) { throw new IOException("Invalid local callback record", invalid); }
    }
    private void validatePayload(long generation) throws IOException {
        if (!scope.equals(kind.equals("reportFederationExecutions") ? "connection" : "member")) throw new IOException("Invalid local callback scope");
        switch (kind) {
            case "informAttributeOwnership":
                RuntimeJson.fields(payload, "object_handle", "attribute_handle", "owner_federate");
                handle(payload, "object_handle"); handle(payload, "attribute_handle"); member(payload, "owner_federate", generation); break;
            case "attributeIsNotOwned": case "attributeIsOwnedByRTI":
                RuntimeJson.fields(payload, "object_handle", "attribute_handle"); handle(payload, "object_handle"); handle(payload, "attribute_handle"); break;
            case "confirmAttributeOwnershipAcquisitionCancellation":
                RuntimeJson.fields(payload, "object_handle", "attribute_handles"); handle(payload, "object_handle"); attributes(payload); break;
            case "confirmAttributeTransportationTypeChange":
                RuntimeJson.fields(payload, "object_handle", "attribute_handles", "transport_type"); handle(payload, "object_handle"); attributes(payload); bounded(payload, "transport_type", 1, 2); break;
            case "confirmInteractionTransportationTypeChange":
                RuntimeJson.fields(payload, "interaction_class_handle", "transport_type"); handle(payload, "interaction_class_handle"); bounded(payload, "transport_type", 1, 2); break;
            case "reportAttributeTransportationType":
                RuntimeJson.fields(payload, "object_handle", "attribute_handle", "transport_type"); handle(payload, "object_handle"); handle(payload, "attribute_handle"); bounded(payload, "transport_type", 1, 2); break;
            case "reportInteractionTransportationType":
                RuntimeJson.fields(payload, "federate_handle", "interaction_class_handle", "transport_type"); member(payload, "federate_handle", generation); handle(payload, "interaction_class_handle"); bounded(payload, "transport_type", 1, 2); break;
            case "federationSaveStatusResponse": case "federationRestoreStatusResponse": {
                boolean restore = kind.equals("federationRestoreStatusResponse"); optional(payload, new String[]{"statuses"}, "state", "active_label");
                if (payload.has("state")) bounded(payload, "state", 1, restore ? 5 : 4);
                if (payload.has("active_label")) RuntimeJson.string(payload, "active_label");
                Set<Long> seen = new HashSet<>();
                for (JsonElement value : RuntimeJson.array(payload, "statuses")) {
                    JsonObject status = value.getAsJsonObject();
                    if (restore) { RuntimeJson.fields(status, "pre_restore_handle", "post_restore_handle", "status"); long post = RuntimeJson.unsigned(status, "post_restore_handle"); if (post != 0 && (int)post == 0) throw new IOException("Invalid historical restore designator"); }
                    else RuntimeJson.fields(status, "federate_handle", "status");
                    long participant = member(status, restore ? "pre_restore_handle" : "federate_handle", generation);
                    if (!seen.add(participant)) throw new IOException("Duplicate local status participant"); bounded(status, "status", 1, restore ? 6 : 4);
                }
                break;
            }
            case "reportFederationExecutions":
                RuntimeJson.fields(payload, "executions");
                Set<String> names = new HashSet<>();
                for (JsonElement value : RuntimeJson.array(payload, "executions")) {
                    JsonObject execution = value.getAsJsonObject(); optional(execution, new String[]{"name", "logical_time_implementation_name"}, "mode", "federates_joined", "federation_generation");
                    String name = RuntimeJson.string(execution, "name"), factory = RuntimeJson.string(execution, "logical_time_implementation_name");
                    if (name.isEmpty() || factory.isEmpty() || !names.add(name)) throw new IOException("Invalid local federation execution");
                    if (execution.has("mode")) bounded(execution, "mode", 0, 2);
                    if (execution.has("federates_joined")) bounded(execution, "federates_joined", 0, 0xffffffffL);
                    if (execution.has("federation_generation")) RuntimeJson.unsigned(execution, "federation_generation");
                }
                break;
            default: throw new IOException("Unsupported local callback kind");
        }
    }
    private static void optional(JsonObject object, String[] required, String... optional) throws IOException {
        Set<String> allowed = new HashSet<>(Arrays.asList(required)); allowed.addAll(Arrays.asList(optional));
        if (!object.keySet().containsAll(Arrays.asList(required)) || !allowed.containsAll(object.keySet())) throw new IOException("Unknown or missing local callback field");
    }
    private static long handle(JsonObject object, String key) throws IOException { long value = RuntimeJson.unsigned(object, key); if (value == 0) throw new IOException("Zero local callback handle"); return value; }
    private static long member(JsonObject object, String key, long generation) throws IOException {
        long value = handle(object, key);
        if (generation != -1 && ((int)value == 0 || value >>> 32 != generation)) throw new IOException("Unbound local callback participant"); return value;
    }
    private static void bounded(JsonObject object, String key, long min, long max) throws IOException {
        long value = RuntimeJson.unsigned(object, key); if (Long.compareUnsigned(value, min) < 0 || Long.compareUnsigned(value, max) > 0) throw new IOException("Invalid local callback enum/count");
    }
    private static void attributes(JsonObject object) throws IOException {
        Set<Long> seen = new HashSet<>();
        for (JsonElement value : RuntimeJson.array(object, "attribute_handles")) { JsonObject wrapper = new JsonObject(); wrapper.add("value", value); if (!seen.add(handle(wrapper, "value"))) throw new IOException("Duplicate local callback attribute"); }
    }
    private static void number(JsonObject object, String key, long value) { object.addProperty(key, new BigInteger(Long.toUnsignedString(value))); }
    private long value(String key) { return Long.parseUnsignedLong(payload.get(key).getAsString()); }

    @Override public void invoke(FederateAmbassador target) throws RTIexception {
        switch (kind) {
            case "informAttributeOwnership": target.informAttributeOwnership(Handles.objectInstance(value("object_handle")), Handles.attribute(value("attribute_handle")), Handles.federate(value("owner_federate"))); break;
            case "attributeIsNotOwned": target.attributeIsNotOwned(Handles.objectInstance(value("object_handle")), Handles.attribute(value("attribute_handle"))); break;
            case "attributeIsOwnedByRTI": target.attributeIsOwnedByRTI(Handles.objectInstance(value("object_handle")), Handles.attribute(value("attribute_handle"))); break;
            case "confirmAttributeOwnershipAcquisitionCancellation": target.confirmAttributeOwnershipAcquisitionCancellation(Handles.objectInstance(value("object_handle")), attributeSet()); break;
            case "confirmAttributeTransportationTypeChange": target.confirmAttributeTransportationTypeChange(Handles.objectInstance(value("object_handle")), attributeSet(), Handles.transportation((int)value("transport_type"))); break;
            case "confirmInteractionTransportationTypeChange": target.confirmInteractionTransportationTypeChange(Handles.interactionClass(value("interaction_class_handle")), Handles.transportation((int)value("transport_type"))); break;
            case "reportAttributeTransportationType": target.reportAttributeTransportationType(Handles.objectInstance(value("object_handle")), Handles.attribute(value("attribute_handle")), Handles.transportation((int)value("transport_type"))); break;
            case "reportInteractionTransportationType": target.reportInteractionTransportationType(Handles.federate(value("federate_handle")), Handles.interactionClass(value("interaction_class_handle")), Handles.transportation((int)value("transport_type"))); break;
            case "federationSaveStatusResponse": {
                JsonArray rows = payload.getAsJsonArray("statuses"); FederateHandleSaveStatusPair[] result = new FederateHandleSaveStatusPair[rows.size()];
                for (int i = 0; i < rows.size(); i++) { JsonObject row = rows.get(i).getAsJsonObject(); result[i] = new FederateHandleSaveStatusPair(Handles.federate(Long.parseUnsignedLong(row.get("federate_handle").getAsString())), SaveStatus.values()[row.get("status").getAsInt() - 1]); }
                target.federationSaveStatusResponse(result); break;
            }
            case "federationRestoreStatusResponse": {
                JsonArray rows = payload.getAsJsonArray("statuses"); FederateRestoreStatus[] result = new FederateRestoreStatus[rows.size()];
                for (int i = 0; i < rows.size(); i++) { JsonObject row = rows.get(i).getAsJsonObject(); long post = Long.parseUnsignedLong(row.get("post_restore_handle").getAsString()); result[i] = new FederateRestoreStatus(Handles.federate(Long.parseUnsignedLong(row.get("pre_restore_handle").getAsString())), post == 0 ? null : Handles.federate(post), RestoreStatus.values()[row.get("status").getAsInt() - 1]); }
                target.federationRestoreStatusResponse(result); break;
            }
            case "reportFederationExecutions": {
                InformationSet result = new InformationSet();
                for (JsonElement value : payload.getAsJsonArray("executions")) { JsonObject row = value.getAsJsonObject(); result.add(new FederationExecutionInformation(row.get("name").getAsString(), row.get("logical_time_implementation_name").getAsString())); }
                target.reportFederationExecutions(result); break;
            }
            default: throw new RTIinternalError("Unsupported local callback kind");
        }
    }
    private AttributeHandleSet attributeSet() {
        AttributeHandleSet result = HandleCollections.attributeSetFactory().create();
        for (JsonElement value : payload.getAsJsonArray("attribute_handles")) result.add(Handles.attribute(Long.parseUnsignedLong(value.getAsString()))); return result;
    }
    private static final class InformationSet extends HashSet<FederationExecutionInformation> implements FederationExecutionInformationSet { private static final long serialVersionUID = 1L; }
}
