import { useCallback } from 'react';

import { trackInteraction, type Interaction } from './interaction';

/**
 * Returns a tracker bound to the current view's route template.
 *
 *   const track = useTrackedAction('/policies/:policyId');
 *   const onSave = () => track('save_policy', async (ix) => { ... });
 */
export function useTrackedAction(route: string) {
  return useCallback(
    <T,>(action: string, fn: (ix: Interaction) => Promise<T> | T) => trackInteraction(action, route, fn),
    [route],
  );
}
