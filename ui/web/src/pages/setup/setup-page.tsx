import { useState } from "react";
import { Navigate, useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { ROUTES } from "@/lib/constants";
import { queryKeys } from "@/lib/query-keys";
import { useFirstRunGate } from "./hooks/use-first-run-gate";
import { SetupLayout } from "./setup-layout";
import { SetupStepper } from "./setup-stepper";
import { StepProvider } from "./step-provider";
import { StepModel } from "./step-model";
import { StepAgent } from "./step-agent";
import { SetupCompleteModal } from "./setup-complete-modal";
import type { ProviderData } from "@/types/provider";
import type { AgentData } from "@/types/agent";

type StepNum = 1 | 2 | 3;

export function SetupPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { needsSetup, loading } = useFirstRunGate();
  const [step, setStep] = useState<StepNum>(1);
  const [provider, setProvider] = useState<ProviderData | null>(null);
  const [modelId, setModelId] = useState<string | null>(null);
  const [showComplete, setShowComplete] = useState(false);

  // If gate already disengaged (provider + agent exist), kick to overview.
  // The gate also runs in RequireAuth; this is the direct-URL fallback so
  // typing /setup with a configured DB doesn't strand the user.
  if (!loading && !needsSetup && !showComplete) {
    return <Navigate to={ROUTES.OVERVIEW} replace />;
  }

  const completedSteps: number[] = [];
  if (step > 1) completedSteps.push(1);
  if (step > 2) completedSteps.push(2);
  if (showComplete) completedSteps.push(1, 2, 3);

  return (
    <SetupLayout>
      <SetupStepper currentStep={step} completedSteps={completedSteps} />

      {step === 1 && (
        <StepProvider
          onComplete={(p) => {
            setProvider(p);
            setStep(2);
          }}
        />
      )}

      {step === 2 && provider && (
        <StepModel
          provider={provider}
          onBack={() => setStep(1)}
          onComplete={(m) => {
            setModelId(m);
            setStep(3);
          }}
        />
      )}

      {step === 3 && provider && modelId && (
        <StepAgent
          provider={provider}
          modelId={modelId}
          onBack={() => setStep(2)}
          onComplete={(_agent: AgentData) => {
            setShowComplete(true);
          }}
        />
      )}

      <SetupCompleteModal
        open={showComplete}
        onGoToOverview={() => {
          // Invalidate cached lists so AppLayout sees the new provider+agent.
          queryClient.invalidateQueries({ queryKey: queryKeys.providers.all });
          queryClient.invalidateQueries({ queryKey: queryKeys.agents.all });
          navigate(ROUTES.OVERVIEW, { replace: true });
        }}
      />
    </SetupLayout>
  );
}
