// DO NOT EDIT. This file is automatically generated.

export interface Annotation {
	Message: string;
	User: string;
	Timestamp: string;
}

export interface Description {
	Mode: Mode;
	Annotation: Annotation;
	Note: Annotation;
	Dimensions: SwarmingDimensions;
	PodName: string;
	KubernetesImage: string;
	ScheduledForDeletion: string;
	PowerCycle: boolean;
	LastUpdated: string;
	Battery: number;
	Temperature: { [key: string]: number } | null;
	RunningSwarmingTask: boolean;
	RecoveryStart: string;
	DeviceUptime: number;
}

export type Mode = "available" | "maintenance" | "recovery";

export type SwarmingDimensions = { [key: string]: string[] | null } | null;
