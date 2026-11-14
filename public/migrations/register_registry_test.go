package migrations

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

var retainedSQLArchives = map[string]struct {
	canonical       string
	archiveSHA256   string
	canonicalSHA256 string
}{
	"191_i18n_seed_product_inventory.sql": {
		"723b_current_191_i18n_seed_product_inventory.sql",
		"0640f454e3ae372080c58cb10e7b9b54123bdadd5640c28aea8edf0579c90080",
		"46bf2810cb62be5589b3f4c7b1d521e54def4f1e8f0912077e34135f3868899f",
	},
	"229_i18n_seed_admin_page_shell.sql": {
		"723i_current_229_i18n_seed_admin_page_shell.sql",
		"7acd4ed4cc2e44cdcf3ff817546ce3c64d83f64bd6ef6e06f64f0f8837e19f32",
		"df8fc39003a5e12618bed1d4782427bcd666f65cb200ff92e2868b9e592fa87c",
	},
	"230_i18n_seed_list_skeleton.sql": {
		"720c_current_230_i18n_seed_list_skeleton.sql",
		"30752879c17afee78c694678376d94676794cbd1360b3bd1e23b4ee347a91be0",
		"88dc0a11aa2049e57d9673b446046be7540ac41daa2f070a5d6b3a6c6eb643b7",
	},
	"231_i18n_seed_list_skeleton_two.sql": {
		"723j_current_231_i18n_seed_list_skeleton_two.sql",
		"765781b7577069648e93bf610e3f0dcb4e1161901785a3eac9fa78ea9dd3bd7a",
		"94d981119cbea55edf7370d3fcb218c55e4a6ae58a43adea8b6c57ec0fcfdd25",
	},
	"232_i18n_seed_admin_pages_round4.sql": {
		"723k_current_232_i18n_seed_admin_pages_round4.sql",
		"742d0051388bd7a69b5c47bc8c957c779644180381d818a8c942515f5678a412",
		"c6c1fc8add791feb2c5dfef55e8a331d90907102b5a00ac8977bc03d5bca2e48",
	},
	"233_i18n_seed_product_detail.sql": {
		"723l_current_233_i18n_seed_product_detail.sql",
		"9765d5c55df0650e1edfb2332f407e19b7ec68283cfdef44c30bbe510b235320",
		"8a781ed5a688e62275a35f000f6a5cf60bd3a8f0d275e967427da2726d88f431",
	},
	"247_i18n_seed_product_followups.sql": {
		"723e_current_247_i18n_seed_product_followups.sql",
		"de9a2c30d7cbe574ae5e611fffd776d3b26d9181898afeec9e2a1e94e054d053",
		"69a307039d0cb3cb63cbc3a0bdae537f121d5b7fc9944a97e73bc8d6b7411252",
	},
	"248_product_sku_error_i18n.sql": {
		"723f_current_248_product_sku_error_i18n.sql",
		"5a561b2024f6a5ace37c730dcc9312ec5a374803c9c045ea9eaff179d6729ca1",
		"a3182148a1c7ad39bdc3aaa80a493a5d1a05c22869ce69650fd8d7892105ca73",
	},
	"252_i18n_seed_inventory_external_sku.sql": {
		"723g_current_252_i18n_seed_inventory_external_sku.sql",
		"1e624536e691dd98526b3c5136a8255f08086eb87f0cac72daca2bddb7530e26",
		"a531eca13f350cd2ff2ca273ddba308dd9552d5ce8b914180581aad887f206af",
	},
	"253_product_variant_list_i18n.sql": {
		"723m_current_253_product_variant_list_i18n.sql",
		"f7160e80094ddb330ca03d20243257b07ff9dbaf57476b4f7e77202bb8d405d2",
		"1f5959ce79b2eae6956d84f46483a5f3fe17f7e99a7b9761784658f7c926bba7",
	},
	"264_product_warehouse_stock_i18n.sql": {
		"723n_current_264_product_warehouse_stock_i18n.sql",
		"f19326c76bca32aadd687c7b99ec95eb8df729cc5ba7d63cd1f5e5434a88a8ba",
		"43619f265279d58b3e12060858c62b49f04cf81250ccf2f68fcfee53d74a8fd6",
	},
	"283_bulk_notice_i18n.sql": {
		"718e_current_283_bulk_notice_i18n.sql",
		"321239f97775c66bb1a92e51b7b39aeca50df7e25fc5ce28f0b465f4beb1d7ef",
		"48666f62466f46628bc4547014cd0ebd35d5e7e1950b6683ac24c82816eb5dc0",
	},
	"284_admin_remaining_templates_i18n.sql": {
		"723c_current_284_admin_remaining_templates_i18n.sql",
		"790444d61406c5c850061016ada02dd68ba8379f951296c895b92835579acd9f",
		"ae605a696e26e5dd82b1eeb948c4d15d99a5e36da802d0dce592985d9411edb9",
	},
	"304_i18n_seed_product_related_ids.sql": {
		"718f_current_304_i18n_seed_product_related_ids.sql",
		"8a2f9d63707b269325dc63f16dd4a28743e28ef3013d6858cfc93eef13add0df",
		"c82136aeac561e65b02c8d40fd6591a26228c7bfe5c5c77ac48d14c872bee967",
	},
	"312_i18n_seed_product_tag_cross_project.sql": {
		"718g_current_312_i18n_seed_product_tag_cross_project.sql",
		"7516727d89e01e45083ee5dbf4344b7f922bdfb25d806a16aa85c88bfdbd67d9",
		"3ad5b737e074b757eb2b081bb74a1c299d1a83745b9595db603c173edca6573e",
	},
	"314_i18n_seed_product_edit.sql": {
		"723v_current_314_i18n_seed_product_edit.sql",
		"9ed064e359ce108971421ba797071f321109fac5296e7c93548e5540601de059",
		"403f873494c409746bd4a673555d40ab91c1035fcbaf4103ffb456d8d08fea74",
	},
	"315_i18n_seed_product_readonly.sql": {
		"723ac_current_315_i18n_seed_product_readonly.sql",
		"98e8e9a14762e7b70a322ec6c362fc05dff1e01465c01b97268e2a0bf6070a79",
		"8da48482907fa2cfd6b5c61b4750d9b55b081b2ee43f6c61ba386ca9c4332344",
	},
	"407_i18n_product_inventory_page_err.sql": {
		"747d_current_407_i18n_product_inventory_page_err.sql",
		"a723a92efe3e94c863ed2c15a5531285bbbec467824254bdcde72af8e2212ba5",
		"7f31bf69770646a0759fb618a6b5d4e9263c39c90a2d8301cf0ed87b81ad3fc2",
	},
	"411_i18n_product_list_filter.sql": {
		"721e_current_411_i18n_product_list_filter.sql",
		"1a4aabfc5a449f42fc6db5a0cfdccf2ffe737269bab9509c2b1f3e3515577b7d",
		"741a88efb460078ee6b5789a7315276c8a43a923b5e659b04d4019c772ae0bcf",
	},
	"430_i18n_enums_missing_keys.sql": {
		"718i_current_430_i18n_enums_missing_keys.sql",
		"cc2027ac0d742114a760d1c70e44013cddcf8327d43ab7bffd90b3d6c5a72f7d",
		"828f9ffc1d670c69a9d6a3cb6aefda02a8c32324fd5465a70a1912c1d9d75a5e",
	},
	"432_i18n_admin_batch_i18n.sql": {
		"723a_current_432_i18n_admin_batch_i18n.sql",
		"c5962779c4e3f0010d428dc87f63c0c6b0b4cb4850738721eca484a1cd20fc1e",
		"fe0998c929f54a5b5f31d565c1e48b9a87f588b8be716060579599557f92c8f7",
	},
	"434_i18n_bulk_confirm.sql": {
		"721b_current_434_i18n_bulk_confirm.sql",
		"9f126c0b2fd751c67d83ba8cd4743fd318fc55894f8f948139f84ebcc078cd27",
		"bb996ca182f9ee1e1277f2b48bfebef991b73a142e5162336ca0669b95be9e10",
	},
	"435_i18n_tail_hardcoded.sql": {
		"720i_current_435_i18n_tail_hardcoded.sql",
		"a466e053c3feac8e3eb13c04265d81c5c0d7966917e8d8018b1ae218e6b13655",
		"af0ad5a142bd01a2ae39a6e1e12471d14ecb735a19e6820d26cd54848eb34dbb",
	},
	"438_i18n_product_rating_count_header.sql": {
		"723ab_current_438_i18n_product_rating_count_header.sql",
		"e48abc779735c7166d13226c2d412123bdac0e8129961415476f53f665c392a1",
		"eeef6e26b6e5e1e84b52063ebfb4c07984730952bb51489642899b250d40b1c1",
	},
	"446_i18n_product_category_tree.sql": {
		"720g_current_446_i18n_product_category_tree.sql",
		"74adc401801f316847d726e8299317c2a3a69a203ce80d3d1779a32e875a49d9",
		"4c871ce52b510047a769d672454b0f2a813c1ba44a003d587a8fa62b8be40f40",
	},
	"449_i18n_product_inventory_go_texts.sql": {
		"723w_current_449_i18n_product_inventory_go_texts.sql",
		"0beec98f8be1584ffc6578767a9b2a598ade8e787839e5b64b707acdf7e7dcb9",
		"110b4768cda8501e6a5d47f618a48a8c11669a4844a1bcdf330f4347b1145a36",
	},
	"454_i18n_workbench_fragments.sql": {
		"719b_current_454_i18n_workbench_fragments.sql",
		"40c448f3ab87ee64a9f3eb64dd6a98cdb6c8a1a163dbc01d11e950fcbb011425",
		"e585d86edf46761bfdcc3707893cdc145f31374f81f3ef65a809034c161f3c7c",
	},
	"468_product_comment_policy_i18n.sql": {
		"718k_current_468_product_comment_policy_i18n.sql",
		"c0bdacb6ea5e964cfec0afede5e960430012737bb5891f37b9f02fd67285fb18",
		"f9f161b46c76b304032ea66d715c47352afaf1929d2fb87617d78cdb43b37e96",
	},
	"476_detail_preview_i18n.sql": {
		"723h_current_476_detail_preview_i18n.sql",
		"33c0b2a05ac8361d0b64e749a93979bc7cd4e753fa409afecc8ae4b3457628e3",
		"727182e3b9bf5ba1b6c85bf4e542e20b06ff261fa308a0fac3d37b38f1036c7d",
	},
	"477_preview_open_full_i18n.sql": {
		"723q_current_477_preview_open_full_i18n.sql",
		"3444c2eebb32cc2e01d1bd86fd2e77d6f7c48d48d94e60221dd7de298d40183f",
		"92339e19f9e409741d721fc6608ec0e2fd3f4ea913659ff3980b56dc6d6d0463",
	},
	"478_product_media_gallery_i18n.sql": {
		"723y_current_478_product_media_gallery_i18n.sql",
		"362f4d0427bb92b4cd16860e8d029dc4f5e6d65f31c5402b03fbd8eeb76a8986",
		"354586474f5c5ed7509988104c55b3753d49e332ec3563454d03fefa7fb5f090",
	},
	"479_product_default_image_i18n.sql": {
		"723t_current_479_product_default_image_i18n.sql",
		"2898a0717c0588b5e85cff486126d1e43d76b6e4a484af4342d3037a85083dba",
		"27b255ebe8a02128228901aed32056221bd61185ccbf3b595fc65b46a65a7c9a",
	},
	"481_product_description_i18n.sql": {
		"723u_current_481_product_description_i18n.sql",
		"2df11350b2438f235b4670d45bc403790538d5a1cb8fb47c91d0f35845e10304",
		"4518cb3572049f2b1e2b7a81700d30ef7a6b3b89b33274804b258e9a558f7f15",
	},
	"505_page_langs_i18n_keys.sql": {
		"723p_current_505_page_langs_i18n_keys.sql",
		"a0cb2bf867e081ff9802a291a1aea258bc4689c64e9487eee8ca8c54cf5559f7",
		"7329c70c122c2409472371d3f83b30e67236d580ca7e438c24af6bc54b5f9404",
	},
	"508_product_category_parent_i18n.sql": {
		"720f_current_508_product_category_parent_i18n.sql",
		"f7fd2c0ccee27bbacb2f7ce62b07c762891f171a177b1aeb5ccdfe8408797f13",
		"16c3378f5c085019893030ea5037c476d2ae5649d0167c892a01a9b812984aaf",
	},
	"592_admin_ui_new_keys_i18n.sql": {
		"723d_current_592_admin_ui_new_keys_i18n.sql",
		"0f9931b5654b37fbf69c5d3e3cd5b5af8ad9a89e3cda2c58a1fa84e235501c30",
		"0710fdbdff1c839f7269ea6b4d5b582d83e646627735c9ed57c842c496dabf16",
	},
	"615_product_bulk_ui_i18n.sql": {
		"723s_current_615_product_bulk_ui_i18n.sql",
		"978955e26f6a54e1ea11147900da232a5080841851629979b8a093ba29740d03",
		"92e4542c53098d37f60e435d00ed7a7a94c6e6a1c8aa10af5de166a17e2eddc3",
	},
	"616_seo_score_labels_i18n.sql": {
		"719c_current_616_seo_score_labels_i18n.sql",
		"67a54a871eb7c88ae07d6e7be50b4a8da170a50c3f043c3b648bf5003bc94e49",
		"7aec3920e1ed80994262bb7e6d04851ba512b6b72474358e3c8010c5270fdfef",
	},
	"620_translation_stale_mark_failed_i18n.sql": {
		"718m_current_620_translation_stale_mark_failed_i18n.sql",
		"4364abe6e4cd4630149f77c7613e47c044dbf5b5fbb72436a479d13435c5dc32",
		"5bb44a4fe937dd793cc561c204e5bb1bc63e046a99e350352c9b66341f1a033e",
	},
	"660_product_variant_stock_panel_i18n.sql": {
		"723ad_current_660_product_variant_stock_panel_i18n.sql",
		"c13c6a714c4da438671e85a2333c10041504c21cec2a1ab21ccf69e3ea17aad9",
		"09d0f7574e629b69bdb4afda9832d2a2ef40141392f851c3caf186b18a8300b5",
	},
	"661_product_publish_records_i18n.sql": {
		"723aa_current_661_product_publish_records_i18n.sql",
		"3af40b9cc11d98ccec37936aa52c398a3f1d3384afb0ff42a7f3d9d1a464e33a",
		"e846eced58e52965d6bc00444b802273ef01aafca4aaaad957d857f89781c36c",
	},
	"673b_product_publish_history_i18n.sql": {
		"723z_current_673b_product_publish_history_i18n.sql",
		"1cd3aa1ebaaa5ae0fd495b2f69c8082b72e81f71715226023299ccee4ffe9d13",
		"9584791c02a4e8849b3ebc8c29f650c6e9c1f8fa7d99edfb1bf6efb832a9c1f9",
	},
	"679_blocks_version_preset.sql": {
		"676_blocks_version_preset.sql",
		"884ea197e39ccfe724da045879e758a84a02340ba02ed1ba659fcd8e2ed66cd0",
		"6f1f1718de491e575cfc5ffd5da8dcced9fec1dc2c93d6a4e3fac174b006d66f",
	},
	"680_i18n_block_version_conflict.sql": {
		"677_i18n_block_version_conflict.sql",
		"a6a625ab05ca0c4c5958a855d15cc72696dacf2adec61de010df3d8a42795de5",
		"f1db1f4cb33dd64d08a1805099fbcd94eb41cf745e7c730fa06763248cafb788",
	},
	"695_product_atomic_writes_i18n.sql": {
		"723r_current_695_product_atomic_writes_i18n.sql",
		"fb51e37d18b4e99b20a0ad4919c3d6384fb88ea49988ab42c938ff2bc60dc2f1",
		"438000a1dcdf1ae470ad6ca9e75ffd88e91ff691b637c2cee88ee8460ce8b927",
	},
	"696_product_mvc_facts_i18n.sql": {
		"719d_current_696_product_mvc_facts_i18n.sql",
		"6522bee7e6edd1b4e7b681f24c74c770565ec6298b4bd2eb41e53765b464e568",
		"77fa1df9195ab164b9c19798836e1f04a8db3c1d054730ddc7ddd86266ee42e3",
	},
	"697b_product_error_detail_keys.sql": {
		"718p_current_697b_product_error_detail_keys.sql",
		"feb4cd1807cb500d499b650745b429e8e69545bc9e887b34fede697ad03cc060",
		"00f0ee0013f47ba4352e2e7108a96d1373fbddb26ba1c1c053210013d258fe91",
	},
	"698_product_stock_unknown_i18n.sql": {
		"723o_current_698_product_stock_unknown_i18n.sql",
		"e5f77696f5491247d4344becea6c7eb39f64bb6d25e0729435a4e93e7018d4cd",
		"be5eee35653000157c8928e034867a45f1fd930f1539e977cc434505019663ca",
	},
	"704_product_blocked_errors_i18n.sql": {
		"718q_current_704_product_blocked_errors_i18n.sql",
		"71dac467cfadc7805b489d6a7f19e03e0245dc84ce75060eed62eee0b14a6c87",
		"929e33a7d528e2ad8af38ce1b19154918b39bcb89b98401bb40fc671f8d5b195",
	},
	"058_i18n_seed_enums.sql": {
		"758a_current_058_i18n_seed_enums.sql",
		"eec75d7fd3a811becc60d93f8b189814730741e7bc5949af9c870e8ab97bdceb",
		"534766839be158346c3256af3a3dd65c31233ab6d667d8ddfae5e386f1f839fb",
	},
	// 747a 是 058 的前一版现行源，随 758a 的出现转为精确归档。
	"747a_current_058_i18n_seed_enums.sql": {
		"758a_current_058_i18n_seed_enums.sql",
		"4de2cbe08715ca17c17642897fa091f3330d5651968ef673c9fadabe50f94b2c",
		"534766839be158346c3256af3a3dd65c31233ab6d667d8ddfae5e386f1f839fb",
	},
	"738_shared_error_short_keys_i18n.sql": {
		"747_shared_feedback_short_keys_i18n.sql",
		"5f9caba34e1ebe38b6139d52ccaf4e9ad69ebe56771bb733900c56c888b592a4",
		"02298139aa3721244b1f06a6d68523284b5bb0bf40d4f590c532f5810f66c95c",
	},
	"745_retire_legacy_enum_i18n.sql": {
		"747_shared_feedback_short_keys_i18n.sql",
		"0350f446e8e9af61ed1976700dd94123e0b2aab70254196915b454a226b95ed6",
		"02298139aa3721244b1f06a6d68523284b5bb0bf40d4f590c532f5810f66c95c",
	},
	"405_i18n_block_page_err.sql": {
		"747c_current_405_i18n_block_page_err.sql",
		"60605defafd2ef6cffa9e43a52d58fa9b938259063b45391bc937fdd744ef340",
		"e3b643c1a3583c447641ca84fb4280aa3d1e63982f091128b03c7992a929e1f3",
	},
	"723x_current_407_i18n_product_inventory_page_err.sql": {
		"747d_current_407_i18n_product_inventory_page_err.sql",
		"bb66581eaf6eaa490cae529db82ebc1ad643a21476cbfeb56bfb6cb788e55825",
		"7f31bf69770646a0759fb618a6b5d4e9263c39c90a2d8301cf0ed87b81ad3fc2",
	},
	"720_product_taxonomy_short_keys_i18n.sql": {
		"747e_current_720_product_taxonomy_short_keys_i18n.sql",
		"0cf8de5d470976aed88b29357ea58b56832db50e992f113d24344f4966822ac2",
		"b29a63a0b2b368bb31bdf248cca44916976e60c8992fc02af4e3373578c00fe7",
	},
	"718a_current_191_i18n_seed_product_inventory.sql": {
		"723b_current_191_i18n_seed_product_inventory.sql",
		"60ddd8feb0f21de272bcfdfeb30c630bca7674d017ce01cacfbadbe5f7803375",
		"46bf2810cb62be5589b3f4c7b1d521e54def4f1e8f0912077e34135f3868899f",
	},
	"718b_current_248_product_sku_error_i18n.sql": {
		"723f_current_248_product_sku_error_i18n.sql",
		"cb53d272a7ae56c70725975ec5969cbe0aadf58d2d3a4d3cd276382be4d79bf6",
		"a3182148a1c7ad39bdc3aaa80a493a5d1a05c22869ce69650fd8d7892105ca73",
	},
	"718c_current_253_product_variant_list_i18n.sql": {
		"723m_current_253_product_variant_list_i18n.sql",
		"37179b51b3965f4b514864ff09bb792feaa225a7018f430f7ea209b562d5ee00",
		"1f5959ce79b2eae6956d84f46483a5f3fe17f7e99a7b9761784658f7c926bba7",
	},
	"718d_current_264_product_warehouse_stock_i18n.sql": {
		"723n_current_264_product_warehouse_stock_i18n.sql",
		"2b0e78d6ecd16a6203fb6a4ac4e5703ac88f6de2c6f1fce48afb2a0ddf12e8e3",
		"43619f265279d58b3e12060858c62b49f04cf81250ccf2f68fcfee53d74a8fd6",
	},
	"718h_current_314_i18n_seed_product_edit.sql": {
		"723v_current_314_i18n_seed_product_edit.sql",
		"8280fa8312a8e744def4f3932ee63d8a8cf7cdda48010aebf6d6c91df993714d",
		"403f873494c409746bd4a673555d40ab91c1035fcbaf4103ffb456d8d08fea74",
	},
	"718j_current_449_i18n_product_inventory_go_texts.sql": {
		"723w_current_449_i18n_product_inventory_go_texts.sql",
		"1ea44b416687b1070626bcd7d837b9bd2ee48cb494ed9a0e833c925a33f2536f",
		"110b4768cda8501e6a5d47f618a48a8c11669a4844a1bcdf330f4347b1145a36",
	},
	"718l_current_592_admin_ui_new_keys_i18n.sql": {
		"723d_current_592_admin_ui_new_keys_i18n.sql",
		"2052b2921a4f25b1e57740734f52cc23dfe371ff4883451cd87fc738ab187069",
		"0710fdbdff1c839f7269ea6b4d5b582d83e646627735c9ed57c842c496dabf16",
	},
	"718n_current_695_product_atomic_writes_i18n.sql": {
		"723r_current_695_product_atomic_writes_i18n.sql",
		"cf60b9d6cd51d21a2c33242a9eb7ca1b5f944b1d94fbecb085e800298f051226",
		"438000a1dcdf1ae470ad6ca9e75ffd88e91ff691b637c2cee88ee8460ce8b927",
	},
	"718o_current_696_product_mvc_facts_i18n.sql": {
		"719d_current_696_product_mvc_facts_i18n.sql",
		"9337a2956057e56ece755f36d30b04571739a5eb5a839468d27cba591b397c75",
		"77fa1df9195ab164b9c19798836e1f04a8db3c1d054730ddc7ddd86266ee42e3",
	},
	"719a_current_449_i18n_product_inventory_go_texts.sql": {
		"723w_current_449_i18n_product_inventory_go_texts.sql",
		"d54bca32854aec4ce9d6c1d59d2d950fb6886d86495988bf2d51f58d4aa70889",
		"110b4768cda8501e6a5d47f618a48a8c11669a4844a1bcdf330f4347b1145a36",
	},
	"720a_current_191_i18n_seed_product_inventory.sql": {
		"723b_current_191_i18n_seed_product_inventory.sql",
		"8c49ba0236c66a3409e5468889c2bf66a3c3f4c50314a44c7f787a47935343a1",
		"46bf2810cb62be5589b3f4c7b1d521e54def4f1e8f0912077e34135f3868899f",
	},
	"720b_current_434_i18n_bulk_confirm.sql": {
		"721b_current_434_i18n_bulk_confirm.sql",
		"be1086d6d5f5aa38fbb2da82c077109b7d90ee37355ec7e8ee739108013762fb",
		"bb996ca182f9ee1e1277f2b48bfebef991b73a142e5162336ca0669b95be9e10",
	},
	"720d_current_232_i18n_seed_admin_pages_round4.sql": {
		"723k_current_232_i18n_seed_admin_pages_round4.sql",
		"9753ffe4a5f742850db5bc01492ed8244871ef696a4accbdde24b8877104a214",
		"c6c1fc8add791feb2c5dfef55e8a331d90907102b5a00ac8977bc03d5bca2e48",
	},
	"720e_current_615_product_bulk_ui_i18n.sql": {
		"723s_current_615_product_bulk_ui_i18n.sql",
		"28daf753130d5b636f0e3b29b583375bef37edb9340d91159267ad9fb86571e4",
		"92e4542c53098d37f60e435d00ed7a7a94c6e6a1c8aa10af5de166a17e2eddc3",
	},
	"720h_current_411_i18n_product_list_filter.sql": {
		"721e_current_411_i18n_product_list_filter.sql",
		"fa82d492cf851e2c368743e0e0204de0a0c0c9ec55a750d53ea653cada56055c",
		"741a88efb460078ee6b5789a7315276c8a43a923b5e659b04d4019c772ae0bcf",
	},
	"721a_current_191_i18n_seed_product_inventory.sql": {
		"723b_current_191_i18n_seed_product_inventory.sql",
		"2a5e75c14871c13ac9d5957a1707c64724c441d9a0c7818a56c0a2ddec4b82df",
		"46bf2810cb62be5589b3f4c7b1d521e54def4f1e8f0912077e34135f3868899f",
	},
	"721c_current_232_i18n_seed_admin_pages_round4.sql": {
		"723k_current_232_i18n_seed_admin_pages_round4.sql",
		"bbad49044a58e4247641bb046cd94ba691541953c59642765abc13c53e5b0bae",
		"c6c1fc8add791feb2c5dfef55e8a331d90907102b5a00ac8977bc03d5bca2e48",
	},
	"721d_current_449_i18n_product_inventory_go_texts.sql": {
		"723w_current_449_i18n_product_inventory_go_texts.sql",
		"36758d2736f62f68dfb798d1ce46068edaa212a2b6b54fe8460f2f7cea127027",
		"110b4768cda8501e6a5d47f618a48a8c11669a4844a1bcdf330f4347b1145a36",
	},
	"722a_current_191_i18n_seed_product_inventory.sql": {
		"723b_current_191_i18n_seed_product_inventory.sql",
		"b02939e4ba694c880f849b2f0574648e66f292dcecbd900c648572644b2709c1",
		"46bf2810cb62be5589b3f4c7b1d521e54def4f1e8f0912077e34135f3868899f",
	},
	"722b_current_247_i18n_seed_product_followups.sql": {
		"723e_current_247_i18n_seed_product_followups.sql",
		"1d88fc3703486987da69bb5b4a3ea8967d9f4114ad7edc01724e17e05419d22f",
		"69a307039d0cb3cb63cbc3a0bdae537f121d5b7fc9944a97e73bc8d6b7411252",
	},
	"722c_current_232_i18n_seed_admin_pages_round4.sql": {
		"723k_current_232_i18n_seed_admin_pages_round4.sql",
		"1c8cdfe49e0a4c4c2c805c548972d1077e9bc53aee24f913132de52c34498edb",
		"c6c1fc8add791feb2c5dfef55e8a331d90907102b5a00ac8977bc03d5bca2e48",
	},
	"722d_current_449_i18n_product_inventory_go_texts.sql": {
		"723w_current_449_i18n_product_inventory_go_texts.sql",
		"ef3c503fb78a000a7f3207c95a742f5a519474b55e8cf04f0220782389ac3e1b",
		"110b4768cda8501e6a5d47f618a48a8c11669a4844a1bcdf330f4347b1145a36",
	},
	// 750a 的历史版本播种点分长 key；现行执行版本改为短 key，旧文件只留档不执行。
	"750a_article_imported_i18n.sql": {
		"750a_article_imported_short_keys_i18n.sql",
		"f25c7e62beb54e7c548e8a0f4b685c20d248461dee6760fdc7b5446d09d3a58a",
		"a979185443b400b9c7a360901cc26a186504a9b909d1eceb8c5bdf82702a88ee",
	},
}

func TestEmbeddedSQLFilesAllRegistered(t *testing.T) {
	entries, err := fs.ReadDir(migrationSQLFS, ".")
	if err != nil {
		t.Fatalf("读取嵌入的 SQL 目录失败: %v", err)
	}
	onDisk := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			onDisk[e.Name()] = struct{}{}
		}
	}
	if len(onDisk) == 0 {
		t.Fatal("嵌入的 SQL 列表为空，//go:embed *.sql 可能已失效")
	}
	for name := range onDisk {
		if _, archived := retainedSQLArchives[name]; archived {
			continue
		}
		if _, ok := registeredSQLFiles[name]; !ok {
			t.Errorf("迁移 SQL %s 没有正式登记或精确历史归档", name)
		}
	}
	for name := range registeredSQLFiles {
		if _, ok := onDisk[name]; !ok {
			t.Errorf("注册台账引用了 %s，但嵌入文件系统里没有这个文件", name)
		}
	}
	for name, archive := range retainedSQLArchives {
		if _, executed := registeredSQLFiles[name]; executed {
			t.Errorf("历史归档%s不得再次登记执行，唯一正式文件为%s", name, archive.canonical)
		}
		if _, registered := registeredSQLFiles[archive.canonical]; !registered {
			t.Errorf("历史归档%s对应的正式文件%s没有登记", name, archive.canonical)
		}
		for file, want := range map[string]string{name: archive.archiveSHA256, archive.canonical: archive.canonicalSHA256} {
			raw, readErr := migrationSQLFS.ReadFile(file)
			if readErr != nil {
				t.Errorf("迁移原文%s不可读: %v", file, readErr)
				continue
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != want {
				t.Errorf("迁移历史原文%s被改写: hash=%s want=%s", file, got, want)
			}
		}
	}
	if len(onDisk) != len(registeredSQLFiles)+len(retainedSQLArchives) {
		t.Errorf("磁盘 SQL 数 %d != 正式登记%d + 精确历史归档%d", len(onDisk), len(registeredSQLFiles), len(retainedSQLArchives))
	}
}

func TestSeedVersionsUnique(t *testing.T) {
	seen := make(map[string]string, len(allSeeds))
	for _, s := range allSeeds {
		v := strings.TrimSpace(s.Version)
		if prev, ok := seen[v]; ok {
			t.Errorf("种子版本重复 %q：%s 与 %s", v, prev, s.TableName)
		}
		seen[v] = s.TableName
	}
}

var migrationSlotWhitelist = map[string]string{}

var seedSlotWhitelist = map[string]string{}

func TestMigrationVersionSlotsUnique(t *testing.T) {
	versions := make([]string, 0, len(allMigrations))
	for _, m := range allMigrations {
		versions = append(versions, m.Version)
	}
	checkVersionSlots(t, "迁移", versions, migrationSlotWhitelist)
}

func TestSeedVersionSlotsUnique(t *testing.T) {
	versions := make([]string, 0, len(allSeeds))
	for _, s := range allSeeds {
		versions = append(versions, s.Version)
	}
	checkVersionSlots(t, "种子", versions, seedSlotWhitelist)
}

func checkVersionSlots(t *testing.T, kind string, versions []string, whitelist map[string]string) {
	t.Helper()
	type slot struct {
		major  int
		suffix string
		items  []string
	}
	slots := make(map[string]*slot, len(versions))
	for _, raw := range versions {
		v := strings.TrimSpace(raw)
		major, suffix, err := parseVersionPrefix(v)
		if err != nil {
			t.Errorf("%s版本 %q 解析失败: %v", kind, v, err)
			continue
		}
		key := strconv.Itoa(major) + "|" + suffix
		if slots[key] == nil {
			slots[key] = &slot{major: major, suffix: suffix}
		}
		slots[key].items = append(slots[key].items, v)
	}
	hit := make(map[string]bool, len(whitelist))
	for _, s := range slots {
		if len(s.items) < 2 {
			continue
		}
		majorKey := strconv.Itoa(s.major)
		if reason, ok := whitelist[majorKey]; ok {
			hit[majorKey] = true
			t.Logf("%s主编号 %s 的同号例外（%s）：%v", kind, majorKey, reason, s.items)
			continue
		}
		t.Errorf("%s主编号 %s 被 %d 个版本共用（补丁位都是 %q）：%v —— 同号会让排序退化成整串字典序、"+
			"按编号定位会命中错文件；请改用 086a/086b 这样的补丁位，或在白名单里登记理由",
			kind, majorKey, len(s.items), s.suffix, s.items)
	}
	for majorKey, reason := range whitelist {
		if !hit[majorKey] {
			t.Errorf("白名单里的%s主编号 %s 已经不再冲突（理由：%s）—— 请删掉这条豁免", kind, majorKey, reason)
		}
	}
}
