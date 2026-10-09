export interface Build { Version: string; Revision: string; Label: string; ShortRevision: string; ReleaseURL: string; CommitURL: string }
export interface Notice { Kind: string; Message: string; ActionLabel: string; ActionURL: string }
export interface Base { Title: string; Authenticated: boolean; Username: string; IsAdmin: boolean; CSRFToken: string; CurrentPath: string; Flash: Notice | null }
export interface Option { Value: string; Label: string; Selected: boolean }
export interface Field { Name: string; Label: string; Type: string; Value: string; Placeholder: string; Help: string; Step: string; Min: string; Max: string; Required: boolean; Checked: boolean; Wide: boolean; Options: Option[] | null }
export interface Section { Title: string; Help: string; Class: string; Provider: string; HelpURL: string; HelpLabel: string; Fields: Field[] | null; Advanced: boolean }
export interface HiddenField { Name: string; Value: string }
export interface Action { Label: string; URL: string; Class: string }
export interface PostAction extends Action { Fields: HiddenField[] | null; SelectName: string; SelectLabel: string; SelectOptions: Option[] | null }
export interface LabelValue { Label: string; Value: string }
export interface ResourceCard { Title: string; Subtitle: string; Status: string; StatusClass: string; URL: string; Description: string; Fields: LabelValue[] | null; Actions: Action[] | null; PostActions: PostAction[] | null; Default: boolean }
export interface Lake { ID: string; Name: string; ProviderID: string; Timezone: string; ReleaseTime: string; ReleaseDaysBefore: number; SupportedPasses: string[] }
export interface Connection { Lake: Lake; Configured: boolean; Connected: boolean; SetupStarted: boolean; Status: string; StatusClass: string; Description: string; URL: string; ActionURL: string; ActionLabel: string; DefaultSourceName: string; Fields?: LabelValue[] }
export interface JobRow { ID: number; ShortID: string; ProfileName: string; RequestName: string; Command: string; StatusLabel: string; StatusClass: string; ModeLabel: string; DueLabel: string; CreatedLabel: string }
export interface Job { ID: number; ShortID: string; ProfileName: string; Command: string; StatusLabel: string; StatusClass: string; CreatedLabel: string; StartedLabel: string; FinishedLabel: string; ConfirmationLabel: string; Mode: string; DueLabel: string; ExpiresLabel: string; TimingLabel: string; BookingReview: LabelValue[] | null; Message: string; AwaitingApproval: boolean; CanCancel: boolean }
export interface JobEvent { Time: string; Type: string; Message: string }
export interface AuthData extends Base { Error: string; Message: string }
export interface DashboardData extends Base { BookingCount: number; Jobs: JobRow[] | null; Connections: Connection[] | null; Bookings: { Name: string; URL: string; Date: string }[] | null }
export interface LakesData extends Base { Configured: boolean; Connections: Connection[] | null }
export interface LakeData extends Base { Lake: Lake; Saved: boolean; FormError: string; Sections: Section[]; Connection: Connection; Profiles: ResourceCard[] | null; ProviderName: string; ConnectionError: string; BookingConnectionNotice: string }
export interface BookingsData extends Base { Lakes: { Lake: Lake; Ready: boolean; Problem: string }[] }
export interface QuickBookingData extends Base { Lake: Lake; AccountName: string; Problem: string; FormError: string; Visit: Section; Passes: Section; Confirmation: string }
export interface FormData extends Base { HiddenFields: HiddenField[] | null; SubmitHelp: string; SubmitDisabled: boolean; Eyebrow: string; Heading: string; Description: string; CancelURL: string; ActionURL: string; SubmitLabel: string; FormError: string; Sections: Section[]; SourceSelection: boolean }
export interface ListData extends Base { Eyebrow: string; Heading: string; Description: string; CreateURL: string; CreateLabel: string; EmptyMessage: string; Notice: string; Cards: ResourceCard[] | null }
export interface JobsData extends Base { Jobs: JobRow[] | null }
export interface JobData extends Base { Job: Job; Events: JobEvent[] | null; LastEventID: number }
export interface AccountData extends Base { Error: string; FormUsername: string; PasswordRequired: boolean }
export interface SettingsData extends Base { Sections: Section[]; FormError: string }
export interface NetworkData extends Base { HostCheckEnabled: boolean; AllowedHosts: string; CurrentHost: string; ManagedReason: string; FormError: string }
export interface UserRow { ID: number; Username: string; Role: string; Status: string; StatusClass: string; PasswordState: string; CreatedLabel: string; Editable: boolean }
export interface UsersData extends Base { Users: UserRow[] | null }
export interface UserData extends Base { Heading: string; Description: string; Error: string; FormUsername: string; UserID: number; Creating: boolean; Enabled: boolean; DeleteAllowed: boolean }
export interface ErrorData extends Base { Message: string; ReturnURL: string; ReturnLabel: string }
export interface Pages {
  login: AuthData; setup: AuthData; dashboard: DashboardData; lakes: LakesData; lake: LakeData;
  bookings: BookingsData; quick_booking: QuickBookingData; form: FormData; list: ListData;
  jobs: JobsData; job: JobData; account: AccountData; settings: SettingsData; network_settings: NetworkData;
  users: UsersData; user: UserData; error: ErrorData;
}
export type Page = { [K in keyof Pages]: { Page: K; Data: Pages[K]; Build: Build } }[keyof Pages]
