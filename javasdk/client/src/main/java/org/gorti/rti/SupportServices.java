package org.gorti.rti;

import hla.rti1516e.AttributeHandle;
import hla.rti1516e.FederateHandle;
import hla.rti1516e.ObjectInstanceHandle;
import hla.rti1516e.ResignAction;
import hla.rti1516e.ServiceGroup;
import hla.rti1516e.exceptions.AttributeNotDefined;
import hla.rti1516e.exceptions.InvalidFederateHandle;
import hla.rti1516e.exceptions.InvalidResignAction;
import hla.rti1516e.exceptions.InvalidServiceGroup;
import hla.rti1516e.exceptions.InvalidUpdateRateDesignator;
import hla.rti1516e.exceptions.RTIexception;
import hla.rti1516e.exceptions.RTIinternalError;
import java.util.Objects;
import org.gorti.rti.types.Handles;
import rti.v1.Common;
import rti.v1.Declaration;
import rti.v1.DeclarationServiceGrpc;
import rti.v1.Federation;
import rti.v1.FederationServiceGrpc;
import rti.v1.Support;
import rti.v1.SupportServiceGrpc;

/** Joined support queries and advisory mutations, serialized by the owning ambassador. */
final class SupportServices {
    private static final Common.WireVersion V1 = Common.WireVersion.WIRE_VERSION_V1;
    private final ServiceContext context;
    private final ExchangeServices exchange;

    public SupportServices(ServiceContext context, ExchangeServices exchange) {
        this.context = Objects.requireNonNull(context, "context");
        this.exchange = Objects.requireNonNull(exchange, "exchange");
    }

    public double getUpdateRateValue(String designator) throws RTIexception {
        context.requireRestoreControl();
        if (designator == null) throw new InvalidUpdateRateDesignator("Update rate designator is null");
        Support.GetUpdateRateValueRequest request = Support.GetUpdateRateValueRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setUpdateRateDesignator(designator).build();
        return rate(WireErrors.call(() -> support().getUpdateRateValue(request)).getUpdateRate());
    }

    public double getUpdateRateValueForAttribute(ObjectInstanceHandle object, AttributeHandle attribute) throws RTIexception {
        context.requireRestoreControl();
        long objectId = exchange.knownObject(object);
        long attributeId;
        try { attributeId = Handles.value(attribute); }
        catch (IllegalArgumentException invalid) { throw new AttributeNotDefined("Expected a gorti attribute handle", invalid); }
        Support.GetUpdateRateValueForAttributeRequest request = Support.GetUpdateRateValueForAttributeRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(objectId).setAttributeHandle(attributeId).build();
        return rate(WireErrors.call(() -> support().getUpdateRateValueForAttribute(request)).getUpdateRate());
    }

    public ResignAction getAutomaticResignDirective() throws RTIexception {
        context.requireRestoreControl();
        Federation.GetAutomaticResignDirectiveRequest request = Federation.GetAutomaticResignDirectiveRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate()).build();
        Common.ResignAction action = WireErrors.call(() -> federation().getAutomaticResignDirective(request)).getAction();
        switch (action) {
            case RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES: return ResignAction.UNCONDITIONALLY_DIVEST_ATTRIBUTES;
            case RESIGN_ACTION_DELETE_OBJECTS: return ResignAction.DELETE_OBJECTS;
            case RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP: return ResignAction.CANCEL_PENDING_OWNERSHIP_ACQUISITIONS;
            case RESIGN_ACTION_DELETE_THEN_DIVEST: return ResignAction.DELETE_OBJECTS_THEN_DIVEST;
            case RESIGN_ACTION_CANCEL_THEN_DELETE: return ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST;
            case RESIGN_ACTION_NO_ACTION: return ResignAction.NO_ACTION;
            default: throw new RTIinternalError("Server returned an invalid automatic resign directive");
        }
    }

    public void setAutomaticResignDirective(ResignAction action) throws RTIexception {
        context.requireRestoreControl();
        if (action == null) throw new InvalidResignAction("Resign action is null");
        Common.ResignAction wire;
        switch (action) {
            case UNCONDITIONALLY_DIVEST_ATTRIBUTES: wire = Common.ResignAction.RESIGN_ACTION_UNCONDITIONALLY_DIVEST_ATTRIBUTES; break;
            case DELETE_OBJECTS: wire = Common.ResignAction.RESIGN_ACTION_DELETE_OBJECTS; break;
            case CANCEL_PENDING_OWNERSHIP_ACQUISITIONS: wire = Common.ResignAction.RESIGN_ACTION_CANCEL_PENDING_OWNERSHIP; break;
            case DELETE_OBJECTS_THEN_DIVEST: wire = Common.ResignAction.RESIGN_ACTION_DELETE_THEN_DIVEST; break;
            case CANCEL_THEN_DELETE_THEN_DIVEST: wire = Common.ResignAction.RESIGN_ACTION_CANCEL_THEN_DELETE; break;
            case NO_ACTION: wire = Common.ResignAction.RESIGN_ACTION_NO_ACTION; break;
            default: throw new InvalidResignAction("Unsupported resign action");
        }
        Federation.SetAutomaticResignDirectiveRequest request = Federation.SetAutomaticResignDirectiveRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setAction(wire).build();
        WireErrors.call(() -> federation().setAutomaticResignDirective(request));
    }

    public long normalizeFederateHandle(FederateHandle federate) throws RTIexception {
        context.requireRestoreControl();
        long target;
        try { target = Handles.value(federate); }
        catch (IllegalArgumentException invalid) { throw new InvalidFederateHandle("Expected a gorti federate handle", invalid); }
        Support.NormalizeFederateHandleRequest request = Support.NormalizeFederateHandleRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setTargetHandle(target).build();
        return WireErrors.call(() -> support().normalizeFederateHandle(request)).getValue();
    }

    public long normalizeServiceGroup(ServiceGroup group) throws RTIexception {
        context.requireRestoreControl();
        if (group == null) throw new InvalidServiceGroup("Service group is null");
        int wire;
        switch (group) {
            case FEDERATION_MANAGEMENT: wire = 0; break;
            case DECLARATION_MANAGEMENT: wire = 1; break;
            case OBJECT_MANAGEMENT: wire = 2; break;
            case OWNERSHIP_MANAGEMENT: wire = 3; break;
            case TIME_MANAGEMENT: wire = 4; break;
            case DATA_DISTRIBUTION_MANAGEMENT: wire = 5; break;
            case SUPPORT_SERVICES: wire = 6; break;
            default: throw new InvalidServiceGroup("Unsupported service group");
        }
        Support.NormalizeServiceGroupRequest request = Support.NormalizeServiceGroupRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setServiceGroup(wire).build();
        return WireErrors.call(() -> support().normalizeServiceGroup(request)).getValue();
    }

    public void enableObjectClassRelevanceAdvisorySwitch() throws RTIexception {
        advisory(Declaration.AdvisorySwitchKind.ADVISORY_SWITCH_KIND_OBJECT_CLASS_RELEVANCE, true);
    }

    public void disableObjectClassRelevanceAdvisorySwitch() throws RTIexception {
        advisory(Declaration.AdvisorySwitchKind.ADVISORY_SWITCH_KIND_OBJECT_CLASS_RELEVANCE, false);
    }

    public void enableAttributeRelevanceAdvisorySwitch() throws RTIexception {
        advisory(Declaration.AdvisorySwitchKind.ADVISORY_SWITCH_KIND_ATTRIBUTE_RELEVANCE, true);
    }

    public void disableAttributeRelevanceAdvisorySwitch() throws RTIexception {
        advisory(Declaration.AdvisorySwitchKind.ADVISORY_SWITCH_KIND_ATTRIBUTE_RELEVANCE, false);
    }

    public void enableAttributeScopeAdvisorySwitch() throws RTIexception {
        advisory(Declaration.AdvisorySwitchKind.ADVISORY_SWITCH_KIND_ATTRIBUTE_SCOPE, true);
    }

    public void disableAttributeScopeAdvisorySwitch() throws RTIexception {
        advisory(Declaration.AdvisorySwitchKind.ADVISORY_SWITCH_KIND_ATTRIBUTE_SCOPE, false);
    }

    public void enableInteractionRelevanceAdvisorySwitch() throws RTIexception {
        advisory(Declaration.AdvisorySwitchKind.ADVISORY_SWITCH_KIND_INTERACTION_RELEVANCE, true);
    }

    public void disableInteractionRelevanceAdvisorySwitch() throws RTIexception {
        advisory(Declaration.AdvisorySwitchKind.ADVISORY_SWITCH_KIND_INTERACTION_RELEVANCE, false);
    }

    private void advisory(Declaration.AdvisorySwitchKind kind, boolean enabled) throws RTIexception {
        context.requireMember();
        Declaration.SetAdvisorySwitchRequest request = Declaration.SetAdvisorySwitchRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setSwitchKind(kind).setEnabled(enabled).build();
        WireErrors.call(() -> DeclarationServiceGrpc.newBlockingStub(context.channel()).setAdvisorySwitch(request));
    }

    private static double rate(double value) throws RTIinternalError {
        if (!Double.isFinite(value) || value < 0) throw new RTIinternalError("Server returned an invalid update rate");
        return value;
    }

    private SupportServiceGrpc.SupportServiceBlockingStub support() {
        return SupportServiceGrpc.newBlockingStub(context.channel());
    }

    private FederationServiceGrpc.FederationServiceBlockingStub federation() {
        return FederationServiceGrpc.newBlockingStub(context.channel());
    }
}
